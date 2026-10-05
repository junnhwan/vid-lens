package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"vid-lens/internal/ai"
)

func isCollectionOverview(q string) bool {
	q = strings.ToLower(q)
	return containsAny(q, "整个库", "全库", "全部视频", "所有视频", "whole library", "entire library", "all videos") && containsAny(q, "主题", "概览", "总结", "概括", "共同", "overview", "summar", "theme")
}

type collectionRoute struct {
	Maps       []VideoMap
	Required   []int64
	Dimensions []string
	Overview   string
}

// Summaries are navigation metadata. Page through every authorized member before
// ranking, so source order never prevents a ninth or later video being selected.
func (s *ChatService) routeCollection(ctx context.Context, userID int64, ids []int64, question string, chat ai.ChatClient) (collectionRoute, error) {
	var route collectionRoute
	if s.repos == nil || s.repos.Task == nil {
		return route, nil
	}
	tasks, err := s.repos.Task.ListByIDsForUser(userID, ids)
	if err != nil {
		return route, err
	}
	if len(tasks) != len(ids) {
		return route, errRetrievalScope
	}
	summaries := map[int64]string{}
	const pageSize = 12
	for start := 0; start < len(ids); start += pageSize {
		if err := ctx.Err(); err != nil {
			return route, err
		}
		end := min(start+pageSize, len(ids))
		if s.repos.Summary != nil {
			rows, err := s.repos.Summary.ListNavigationSummaries(ctx, userID, ids[start:end])
			if err != nil {
				return route, err
			}
			for id, content := range rows {
				summaries[id] = content
			}
		}
	}
	overview := isCollectionOverview(question)
	terms := ExtractQueryTerms(question)
	type scored struct {
		id    int64
		score float64
	}
	var ranked []scored
	for _, task := range tasks {
		title := task.Title
		if title == "" {
			title = task.Filename
		}
		score := queryTermCoverage(title+" "+summaries[task.ID], terms)
		if isComparisonQuestion(question) && title != "" && strings.Contains(strings.ToLower(question), strings.ToLower(title)) {
			route.Required = append(route.Required, task.ID)
			score += 2
		}
		ranked = append(ranked, scored{task.ID, score})
	}
	if isComparisonQuestion(question) && len(route.Required) == 0 && len(ids) == 2 {
		route.Required = append(route.Required, ids...)
	}
	// Explicitly named dimensions become independent recall queries, capped by the
	// request budget. Missing source x dimension cells remain visible in the prompt.
	for _, dimension := range []string{"性能", "成本", "恢复", "一致性", "适用条件", "延迟", "可靠性", "优点", "缺点"} {
		if strings.Contains(question, dimension) && len(route.Dimensions) < 3 {
			route.Dimensions = append(route.Dimensions, dimension)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	var selected []int64
	for _, item := range ranked {
		if len(selected) < 8 && (item.score > 0 || containsTaskID(route.Required, item.id) || len(ids) == 1) {
			selected = append(selected, item.id)
		}
	}
	route.Maps, err = s.loadVideoMaps(ctx, userID, selected)
	if err != nil {
		return route, err
	}
	if overview {
		// Read all available summaries in bounded model pages. The digest is navigation,
		// not a quote pool; main claims still need raw retrieved evidence.
		var pages []string
		var processed, missing []int64
		for start := 0; start < len(tasks); start += pageSize {
			var lines []string
			for _, task := range tasks[start:min(start+pageSize, len(tasks))] {
				summary := strings.TrimSpace(summaries[task.ID])
				if summary == "" {
					missing = append(missing, task.ID)
					continue
				}
				processed = append(processed, task.ID)
				lines = append(lines, fmt.Sprintf("视频 %d %s：\n%s", task.ID, task.Title, summary))
			}
			if len(lines) == 0 {
				continue
			}
			text := strings.Join(lines, "\n\n")
			parts := SplitTextIntoChunks(text, 16000, 0)
			for _, part := range parts {
				if len(pages) >= 12 {
					route.Overview = fmt.Sprintf("概要处理达到12页预算；成功处理页面=%d。本次仅为部分概览，后续概要未处理，不能声称完整全库主题。\n%s", len(pages), strings.Join(pages, "\n\n"))
					return route, nil
				}
				navigation := part.Content
				if (len(tasks) > pageSize || len(parts) > 1) && chat != nil {
					navigation, err = chat.Chat(ai.WithChatBudget(ctx, 1800, nil), []ai.ChatMessage{{Role: "system", Content: "将这一页完整视频概要文本按主题归纳为定位导航；保留来源编号、条件、否定和相反观点。不能省略不同视频的独立观点。概要不是原文，不产生引用标记。控制在1600字内。"}, {Role: "user", Content: fmt.Sprintf("这一页的成员范围=%v\n%s", ids[start:min(start+pageSize, len(ids))], part.Content)}})
					if err != nil {
						if ctx.Err() != nil {
							return route, ctx.Err()
						}
						route.Overview = fmt.Sprintf("概要聚合未完成，成功处理页面=%d；本次只提供局部结果，不能声称完整全库概览。", len(pages))
						return route, nil
					}
				}
				pages = append(pages, navigation)
			}

		}
		route.Overview = fmt.Sprintf("全库概要处理范围：可检索成员=%d，已处理概要=%v，无概要=%v。所有可用概要文本已分页读取；概要用于主题导航，不是原文依据。主要结论仍需引用检索原文，未有原文的主题只能标为概要线索。\n%s", len(ids), processed, missing, strings.Join(pages, "\n\n"))
		// Bound final aggregation too, while keeping coverage truthful.
		if len([]rune(route.Overview)) > 24000 && chat != nil {
			aggregated, err := chat.Chat(ctx, []ai.ChatMessage{{Role: "system", Content: "归纳所有页面的主题导航，保留来源、分歧、条件和覆盖信息，不产生引用标记。控制在5000字内。"}, {Role: "user", Content: route.Overview}})
			if err != nil {
				return route, err
			}
			route.Overview = fmt.Sprintf("处理概要=%v，缺概要=%v；聚合仅用于导航。\n%s", processed, missing, aggregated)
		}
	}
	return route, nil
}

func collectionRoutePrompt(route collectionRoute) string {
	raw, _ := json.Marshal(route.Maps)
	return fmt.Sprintf("按问题选择的视频概要与时间线（导航，不能冒充引用原文）：%s\n明确比较目标=%v；必要维度=%v。按维度分别陈述每方原话、共识、矛盾和适用条件。逐个检查来源与维度是否有证据；缺证标注，不从另一方推测。\n%s", raw, route.Required, route.Dimensions, route.Overview)
}

func (route collectionRoute) routedIDs() []int64 {
	var ids []int64
	for _, m := range route.Maps {
		ids = append(ids, m.TaskID)
	}
	return ids
}
