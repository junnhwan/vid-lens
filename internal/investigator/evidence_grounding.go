package investigator

import (
	"slices"
	"strconv"

	"vidlens/internal/domain"
)

const evidencePolicy = "interpretation-2026-10-19-v11"

const evidenceGroundingInstructions = `
逐项核对证据，而不是只核对引用编号：文章、先前问答和持久观察是派生记录，可能有误；与原始文字、实际像素或时间矛盾时以原始来源为准。supported表示观察已记录，不表示每条推断已经成为事实。visible_facts只写像素直接可辨的内容；材质、制作方式、真实身份、动机、主观情绪或因果关系没有直接证据时用中性外观描述，把假设留在inferences，重要未知进入limitations。origin=background_explanation不能把针对本片对象的猜测包装成事实。不要把概括的轻微接触写成像素不支持的具体动作。
画面描述在对应句旁引用fN，旁白条件引用tN；一段同时使用二者就分别引用，不能用整段ASR替代画面来源。用户选定图片的grounded回答须引用本轮question-context的实际fN；这不要求新增figure或重复取帧。摘要也须保留正文的条件与不确定性，不把“部分构造行不可见”扩大成“所有类名不可见”。
frame_text_context/frame_context给出真实帧时间及原始文字的相邻引用，不是额外事实或强制对齐。先核对时间与上下文，再给截图定性：早期输出不能写成修改后的验证；同一份输出的两张截图不能证明修改前后结果一致。若要断言前后相同，须分别有两个阶段的证据；只有口述时明确写“作者称”，不能用旧图冒充后阶段。主题章节可重排，证据发生的先后不能重排。单帧只能证明当时状态，不能证明持续时长、动态反应或未采样过程。粗ASR窗口不能精确对齐其中某句与某帧；不要把一关的时长条件套到另一关。
代码和数值按可见原文核对：区分编辑器建议/未接受diff与实际代码、调用处关键字参数与函数定义、当前文件名与类名。字幕术语有明显识别疑点时结合上下文规范表达并说明不确定性，不能把转写错误宣称为说话者的真实发音或原话。引用选足以支持当前陈述的代表片段，避免堆叠所有重叠字幕。提交前检查每项数字、条件、对象属性、前后阶段及图注；不能支持的断言删去或限定，不用泛泛的免责声明抵消正文中的确定断言。
`

// A local source index, not semantic alignment. A coarse audio window must
// never become a sentence timestamp just because a frame falls inside it.
func frameTextContext(at int64, transcript []domain.Segment) map[string]any {
	overlap, coarse := []string{}, []string{}
	nearest, distance := -1, int64(10001)
	for i, segment := range transcript {
		id := "t" + strconv.Itoa(i+1)
		fine := segment.Origin == "platform_subtitles" || segment.Origin == "platform"
		if at >= segment.StartMS && at < segment.EndMS {
			if fine {
				overlap = append(overlap, id)
			} else {
				coarse = append(coarse, id)
			}
		}
		if !fine {
			continue
		}
		d := max(segment.StartMS-at, at-segment.EndMS, int64(0))
		if d < distance {
			nearest, distance = i, d
		}
	}
	if len(overlap) > 0 {
		return map[string]any{"alignment": "platform_cue_overlap_not_visual_entailment", "text_ids": overlap[:min(4, len(overlap))]}
	}
	if len(coarse) > 0 {
		return map[string]any{"alignment": "coarse_audio_window_not_sentence_timing", "text_ids": coarse[:min(4, len(coarse))]}
	}
	if nearest >= 0 {
		return map[string]any{"alignment": "nearby_platform_cue_not_exact_alignment", "text_ids": []string{"t" + strconv.Itoa(nearest+1)}}
	}
	return map[string]any{"alignment": "no_nearby_text", "text_ids": []string{}}
}

func frameContexts(catalog map[string]domain.Citation, transcript []domain.Segment) map[string]any {
	out := map[string]any{}
	for alias, c := range catalog {
		if c.Kind == "video_frame" && c.TimeMS != nil {
			hint := frameTextContext(*c.TimeMS, transcript)
			hint["frame_id"], hint["time_ms"] = c.FrameID, *c.TimeMS
			out[alias] = hint
		}
	}
	return out
}

// Add only canonical source links for the exact displayed pixels. This does
// not assert that the caption's semantic claims are supported by those pixels.
func linkFigureFrames(section domain.Section, catalog map[string]domain.Citation) domain.Section {
	section.Blocks = slices.Clone(section.Blocks)
	for i := range section.Blocks {
		b := &section.Blocks[i]
		if b.Type != "figure" {
			continue
		}
		b.CitationIDs = slices.Clone(b.CitationIDs)
		for _, frame := range b.FrameIDs {
			alias := ""
			if c, ok := catalog[frame]; ok && c.Kind == "video_frame" {
				alias = frame
			} else {
				// Raw owned frame IDs remain accepted by the historical body contract.
				// Pick a stable alias even if several entries identify the same asset.
				for id, c := range catalog {
					if c.Kind == "video_frame" && c.FrameID == frame && (alias == "" || id < alias) {
						alias = id
					}
				}
			}
			if alias != "" && !slices.Contains(b.CitationIDs, alias) {
				b.CitationIDs = append(b.CitationIDs, alias)
			}
		}
	}
	return section
}

func (s *session) validateQuestionFrameReference(a domain.Answer) error {
	if !s.policy().EvidenceGrounding || s.turn == nil || a.Kind != "grounded" || len(s.cp.Inspections["question-context"]) == 0 {
		return nil
	}
	used := map[string]bool{}
	for _, b := range a.Blocks {
		for _, id := range blockCitationIDs(b) {
			used[id] = true
		}
	}
	for _, l := range a.Limitations {
		for _, id := range l.CitationIDs {
			used[id] = true
		}
	}
	for id := range used {
		c := a.Citations[id]
		if c.Kind == "video_frame" && slices.Contains(s.cp.Inspections["question-context"], c.FrameID) {
			return nil
		}
	}
	return &domain.Problem{Status: 422, Code: "MODEL_RESULT_INVALID", Detail: "用户选定的图片已交付并观察；请在相关画面描述或限制中引用对应fN，不能只引用旁白。无需新增取帧或figure。", Meta: map[string]any{"code": "question_frame_uncited", "path": "/blocks", "repairable": true}}
}
