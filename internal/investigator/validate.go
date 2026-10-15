package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

type candidateError struct{ err error }

func (e *candidateError) Error() string { return e.err.Error() }
func invalid(msg string) error {
	return domain.Err(422, "EVIDENCE_INSUFFICIENT", "结果校验未通过："+msg)
}
func (s *session) validateArticle(ctx context.Context, a domain.Article) error {
	if strings.TrimSpace(a.Title) == "" || len(a.Sections) == 0 || len(a.Sections) > 40 {
		return invalid("缺少文章标题或章节")
	}
	if len(s.unobserved(ctx)) > 0 {
		return invalid("还有图片观察未记录")
	}
	obs, e := s.observations(ctx)
	if e != nil {
		return e
	}
	latest := map[string]observation{}
	observed := map[string]bool{}
	for _, o := range obs {
		latest[o.QuestionID] = o
		for _, id := range o.FrameIDs {
			observed[id] = true
		}
	}
	for _, id := range s.cp.OverviewIDs {
		if !observed[id] {
			return invalid("全片概览尚有未记录的画面")
		}
	}
	for _, o := range latest {
		if o.Critical && o.Status != "supported" {
			return invalid("关键取证问题尚未解决")
		}
	}
	questions, e := s.engine.Store.Questions(ctx, s.claim.Run.ID)
	if e != nil {
		return e
	}
	for _, raw := range questions {
		var q struct {
			Critical bool
			Status   string
		}
		if json.Unmarshal(raw, &q) != nil {
			return invalid("问题状态无效")
		}
		if q.Critical && q.Status != "supported" {
			return invalid("关键取证问题尚未得到支持")
		}
	}
	sectionIDs := map[string]bool{}
	blocks := []domain.Block{}
	for _, sec := range a.Sections {
		if sec.ID == "" || sectionIDs[sec.ID] || sec.Title == "" || len(sec.Blocks) == 0 {
			return invalid("章节结构无效")
		}
		sectionIDs[sec.ID] = true
		blocks = append(blocks, sec.Blocks...)
	}
	if e = s.validateBlocks(ctx, blocks, a.Citations, true); e != nil {
		return e
	}
	figures := 0
	prose := 0
	for _, b := range blocks {
		if b.Type == "figure" {
			figures++
		}
		if b.Type == "paragraph" || b.Type == "list" {
			prose += len(domain.BlockText(b))
		}
	}
	if figures == 0 || prose < 40 {
		return invalid("缺少可阅读正文或真实配图")
	}
	if e = validateLimitations(a.Limitations, sectionIDs, a.Citations); e != nil {
		return e
	}
	prov := a.Source.TextProvenance
	if prov == nil || prov.Coverage != "complete" {
		has := false
		for _, l := range a.Limitations {
			has = has || l.Code == "partial_text"
		}
		if !has {
			return invalid("不完整的文字来源必须声明 partial_text")
		}
	}
	if a.MindMap != nil {
		if e = validateMap(a.MindMap, sectionIDs); e != nil {
			return e
		}
	}
	return nil
}
func (s *session) validateAnswer(ctx context.Context, a *domain.Answer) error {
	if a.Kind != "grounded" && a.Kind != "insufficient_evidence" && a.Kind != "out_of_scope" {
		return invalid("回答 kind 无效")
	}
	if a.Summary == "" || len(a.Blocks) == 0 {
		return invalid("缺少答案")
	}
	if len(s.unobserved(ctx)) > 0 {
		return invalid("答案引用的图片尚未完成观察")
	}
	if e := s.validateBlocks(ctx, a.Blocks, a.Citations, false); e != nil {
		return e
	}
	sections := map[string]bool{}
	for _, sec := range s.article.Sections {
		sections[sec.ID] = true
	}
	if e := validateLimitations(a.Limitations, sections, a.Citations); e != nil {
		return e
	}
	ids := storeFrameIDs(a.Blocks, a.Citations)
	frames, e := s.engine.Store.PublicFrames(ctx, s.turn.RequestID, ids)
	if e != nil {
		return e
	}
	a.Frames = frames
	return nil
}
func storeFrameIDs(blocks []domain.Block, c map[string]domain.Citation) []string {
	ids := []string{}
	for _, b := range blocks {
		for _, id := range b.FrameIDs {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	for _, x := range c {
		if x.FrameID != "" && !slices.Contains(ids, x.FrameID) {
			ids = append(ids, x.FrameID)
		}
	}
	return ids
}
func (s *session) validateBlocks(ctx context.Context, blocks []domain.Block, citations map[string]domain.Citation, article bool) error {
	if len(blocks) > 400 {
		return invalid("正文块数量超限")
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		if b.ID == "" || seen[b.ID] {
			return invalid("正文块 ID 重复或为空")
		}
		seen[b.ID] = true
		if b.Origin != "video" && b.Origin != "background_explanation" {
			return invalid("正文来源标识无效")
		}
		ids := append([]string{}, b.CitationIDs...)
		checkInline := func(content []domain.Inline) error {
			if len(content) == 0 {
				return invalid("空行内内容")
			}
			for _, x := range content {
				switch x.Type {
				case "text":
					for _, m := range x.Marks {
						if m != "bold" && m != "emphasis" && m != "code" {
							return invalid("文本样式无效")
						}
					}
				case "citation":
					ids = append(ids, x.CitationID)
				case "link":
					u, e := url.Parse(x.URL)
					if e != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil {
						return invalid("外部链接无效")
					}
				default:
					return invalid("行内内容类型无效")
				}
			}
			return nil
		}
		switch b.Type {
		case "paragraph", "quote":
			if e := checkInline(b.Content); e != nil {
				return e
			}
			if b.Type == "quote" && b.Origin == "video" && len(ids) == 0 {
				return invalid("视频引述缺少来源")
			}
		case "list":
			if b.Ordered == nil || len(b.Items) == 0 {
				return invalid("列表结构无效")
			}
			itemIDs := map[string]bool{}
			for _, x := range b.Items {
				if x.ID == "" || itemIDs[x.ID] {
					return invalid("列表项身份无效")
				}
				itemIDs[x.ID] = true
				if e := checkInline(x.Content); e != nil {
					return e
				}
			}
		case "code":
			if b.Text == "" {
				return invalid("空代码块")
			}
		case "table":
			if len(b.Columns) == 0 || len(b.Rows) == 0 {
				return invalid("空表格")
			}
			for _, r := range b.Rows {
				if len(r.Cells) != len(b.Columns) {
					return invalid("表格列数不一致")
				}
				ids = append(ids, r.CitationIDs...)
			}
		case "figure":
			if b.Origin != "video" || b.Caption == "" || b.Layout != "single" && b.Layout != "sequence" || b.Layout == "single" && len(b.FrameIDs) != 1 || b.Layout == "sequence" && len(b.FrameIDs) < 2 {
				return invalid("配图结构无效")
			}
			framesSeen := map[string]bool{}
			last := int64(-1)
			for _, id := range b.FrameIDs {
				if framesSeen[id] {
					return invalid("配图重复")
				}
				framesSeen[id] = true
				a, e := s.engine.Store.InternalFrame(ctx, s.claim, id)
				if e != nil || a.TimeMS == nil || a.State != "ready" {
					return invalid("配图不是可访问的实际帧")
				}
				if *a.TimeMS < last {
					return invalid("连续帧顺序错误")
				}
				last = *a.TimeMS
				_, _, e = s.engine.Media.ReadImage(ctx, s.claim, id)
				if e != nil {
					return invalid("配图文件不可用")
				}
			}
		default:
			return invalid("正文块类型无效")
		}
		for _, id := range ids {
			if _, ok := citations[id]; !ok {
				return invalid("引用不存在：" + id)
			}
		}
	}
	for id, c := range citations {
		if id != c.ID || id == "" || c.Label == "" {
			return invalid("引用身份无效")
		}
		switch c.Kind {
		case "video_text":
			if c.SnapshotID != s.snapshot.ID || c.Range == nil || len(c.SegmentIDs) == 0 || c.Range.StartMS < 0 || c.Range.StartMS >= c.Range.EndMS || c.Range.EndMS > s.snapshot.DurationMS {
				return invalid("字幕引用范围无效")
			}
			selected := []domain.Segment{}
			for _, id := range c.SegmentIDs {
				found := false
				for _, t := range s.transcript {
					if t.ID == id {
						selected = append(selected, t)
						found = true
						break
					}
				}
				if !found {
					return invalid("字幕片段不存在")
				}
			}
			for i := 1; i < len(selected); i++ {
				if selected[i].Ordinal != selected[i-1].Ordinal+1 {
					return invalid("字幕片段不连续")
				}
			}
			if c.Range.StartMS < selected[0].StartMS || c.Range.EndMS > selected[len(selected)-1].EndMS {
				return invalid("引用超出字幕实际窗口")
			}
			origin := selected[0].Origin
			for _, t := range selected {
				if t.Origin != origin {
					origin = "mixed"
				}
			}
			if c.TextOrigin != origin {
				return invalid("字幕来源标签不匹配")
			}
			if c.Quote != nil {
				parts := []string{}
				for _, t := range selected {
					parts = append(parts, t.Text)
				}
				if !strings.Contains(domain.NormalizeQuote(strings.Join(parts, "\n")), domain.NormalizeQuote(*c.Quote)) {
					return invalid("引用原文不匹配")
				}
			}
		case "video_frame":
			a, e := s.engine.Store.InternalFrame(ctx, s.claim, c.FrameID)
			if e != nil || a.TimeMS == nil || c.TimeMS == nil || c.SnapshotID != s.snapshot.ID || *a.TimeMS != *c.TimeMS {
				return invalid("画面来源或真实时间不匹配")
			}
			if article {
				obs, e := s.observations(ctx)
				if e != nil {
					return e
				}
				seen := false
				for _, o := range obs {
					seen = seen || slices.Contains(o.FrameIDs, c.FrameID)
				}
				if !seen {
					return invalid("配图没有已保存观察")
				}
			}
		case "article_passage":
			if article || s.article == nil || c.ArticleID != s.article.ID || c.Quote == nil {
				return invalid("文章引用无效")
			}
			valid := false
			for _, sec := range s.article.Sections {
				for _, b := range sec.Blocks {
					if sec.ID == c.SectionID && b.ID == c.BlockID && strings.Contains(domain.NormalizeQuote(domain.BlockText(b)), domain.NormalizeQuote(*c.Quote)) {
						valid = true
					}
				}
			}
			if !valid {
				return invalid("文章选段不匹配")
			}
		default:
			return invalid("引用类型无效")
		}
	}
	return nil
}
func validateLimitations(ls []domain.Limitation, sections map[string]bool, c map[string]domain.Citation) error {
	for _, l := range ls {
		if l.Message == "" {
			return invalid("限制说明为空")
		}
		if !slices.Contains([]string{"visual_unclear", "source_conflict", "not_observed", "partial_text", "media_unavailable", "resource_limit"}, l.Code) {
			return invalid("限制类型无效")
		}
		for _, id := range l.SectionIDs {
			if !sections[id] {
				return invalid("限制引用的章节不存在")
			}
		}
		for _, id := range l.CitationIDs {
			if _, ok := c[id]; !ok {
				return invalid("限制引用不存在")
			}
		}
	}
	return nil
}
func validateMap(m *domain.MindMap, sections map[string]bool) error {
	nodes := map[string]domain.MapNode{}
	roots := 0
	for _, n := range m.Nodes {
		if n.ID == "" || n.Label == "" {
			return invalid("导图节点为空")
		}
		if _, ok := nodes[n.ID]; ok {
			return invalid("导图节点重复")
		}
		if n.ParentID == nil {
			roots++
		}
		if n.SectionID != nil && !sections[*n.SectionID] {
			return invalid("导图章节不存在")
		}
		nodes[n.ID] = n
	}
	root, ok := nodes[m.RootID]
	if !ok || root.ParentID != nil || roots != 1 {
		return invalid("导图根无效")
	}
	for _, n := range nodes {
		visited := map[string]bool{}
		for n.ParentID != nil {
			if visited[n.ID] {
				return invalid("导图有环")
			}
			visited[n.ID] = true
			var found bool
			n, found = nodes[*n.ParentID]
			if !found {
				return invalid("导图父节点不存在")
			}
		}
		if n.ID != m.RootID {
			return invalid("导图不连通")
		}
	}
	return nil
}
func (s *session) repair(ctx context.Context, cause error) error {
	if s.cp.Repairs >= 1 {
		return invalid("修复一次后仍不符合结果格式")
	}
	s.cp.Repairs++
	last := -1
	for i := len(s.cp.Messages) - 1; i >= 0; i-- {
		if s.cp.Messages[i].Role == "assistant" {
			last = i
			break
		}
	}
	if last < 0 {
		return errors.New("missing candidate call")
	}
	for _, call := range s.cp.Messages[last].ToolCalls {
		s.cp.Messages = append(s.cp.Messages, &schema.Message{Role: schema.Tool, ToolName: call.Function.Name, ToolCallID: call.ID, Content: fmt.Sprintf("候选校验失败，允许修复一次：%v。修正字段/引用后重新提交。", cause)})
	}
	return s.persist(ctx, s.cp.Messages)
}
