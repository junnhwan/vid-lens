package service

import (
	"regexp"
	"strings"
)

// docs/architecture/retrieval.md: 无副作用 Signal 提取（指代消解 + 检索过滤）。
//
// Signal = 纯正则从问题里提取的结构化线索（CONTEXT.md）：时间戳（毫秒区间）、
// 实体、章节、比较标志。实体和句式可参与分类；时间戳在 timeline_locate 下用于
// 过滤具有可信持久化范围的 chunk（见 docs/architecture/retrieval.md）。
//
// 与 rag_rewrite LLM 改写边界（见 docs/architecture/retrieval.md，两层正交）：
//   - Signal 无副作用：纯正则，不调 LLM、不编造，先于 rewrite 提取结构化线索。
//   - rewrite 有副作用：LLM 改写，可能补全/编造语义，后于 Signal。
// 两层分工——Signal 给"可确定的结构化线索"，rewrite 给"语义级改写"，互不替代。
//
// 无副作用约束（反 overclaim）：本文件零 LLM 调用、零外部依赖，所有提取纯靠
// 正则匹配。任何"Signal 智能解析实体"的表述都是拔高——它就是正则。

// TimestampRange 是一个问题里提到的时间区间（毫秒），用于缩检索 chunk 的时间范围。
// 单点时间戳（"第15分钟"）解析成 [start, start]；区间（"15分到20分"）解析成 [a, b]。
type TimestampRange struct {
	StartMS int64
	EndMS   int64
	// Raw 是匹配到的原始文本，供调试/audit（不进检索逻辑）。
	Raw string
}

// Signals 是一次提问的无副作用结构化线索。
type Signals struct {
	// Timestamps 是问题里提到的所有时间戳区间（毫秒）。只有来源映射保存了可信
	// 范围的 chunk 才能匹配；unknown 历史数据不会被当作命中。
	Timestamps []TimestampRange
	// HasCompare 命中比较句式（"对比"/"比较"/"异同"/"区别"），辅助 topic_compare intent。
	HasCompare bool
	// HasOverview 命中概览句式（"讲了什么"/"总结"/"概括"），辅助 video_overview intent。
	HasOverview bool
	// HasSmallTalk 命中闲聊句式（"你好"/"谢谢"/"在吗"），辅助 small_talk intent。
	HasSmallTalk bool
	// Entities 是抽取的候选实体（引号包裹 / 大写英文 / 中文专有名词粗提），用于
	// 指代消解回指上文实体。粗提 + 后续 LLM/规则消歧，不保证全是真实体。
	Entities []string
}

// ExtractSignals 从问题文本无副作用提取结构化线索（docs/architecture/retrieval.md）。
//
// 纯正则，零 LLM 调用，零编造。返回的 Signals 用于 RuleIntentClassifier 的 signal
// 模式维度；timeline_locate 会把确定解析出的范围交给检索管线。
func ExtractSignals(question string) Signals {
	q := strings.TrimSpace(question)
	return Signals{
		Timestamps:   extractTimestamps(q),
		HasCompare:   compareSignalRe.MatchString(q),
		HasOverview:  overviewSignalRe.MatchString(q),
		HasSmallTalk: smallTalkSignalRe.MatchString(q),
		Entities:     extractEntities(q),
	}
}

// --- 时间戳正则 ---
//
// 覆盖中文视频问答常见时间表述（见 docs/architecture/retrieval.md）：
//   - "第15分钟" / "第15分" → 单点
//   - "15:00" / "15分30秒" → 单点
//   - "第15到20分钟" / "15:00-20:00" → 区间
//
// audit trail（当前实现约束 硬约束，每个维度写为何这么定）：
//   - 只认"分钟+秒"粒度，不认"小时"：视频问答时间几乎都在分钟级，认小时会引入
//     "02:00" 是凌晨2点还是第2小时的歧义；保守认分钟级，歧义留给 rewrite 兜底。
//   - 单点时间戳解析成 [start, start]：caller 用它做"时间点附近 chunk 过滤"，
//     区间用 [a, b] 做"范围过滤"，两种语义由调用方按 StartMS==EndMS 区分。

var (
	// 第15分钟 / 第15分 / 第15到20分钟 / 第15-20分
	minuteCNRe = regexp.MustCompile(`第?\s*(\d{1,3})\s*[点\-—到至]+\s*(\d{1,3})?\s*分(?:钟)?`)
	// 15:00 / 15:00-20:00 / 15:00—20:00（含区间）
	clockRe = regexp.MustCompile(`(\d{1,2}):(\d{2})(?:\s*[\-—到]+\s*(\d{1,2}):(\d{2}))?`)
	// 15分30秒 / 15分30秒-20分10秒
	minSecCNRe = regexp.MustCompile(`(\d{1,3})\s*分(?:钟)?\s*(\d{1,2})?\s*秒?(?:\s*[\-—到]+\s*(\d{1,3})\s*分(?:钟)?\s*(\d{1,2})?\s*秒?)?`)
)

func extractTimestamps(q string) []TimestampRange {
	if q == "" {
		return nil
	}
	type rawMatch struct {
		TimestampRange
		idx     int
		isRange bool
	}
	collect := func(re *regexp.Regexp, build func(m []string) (tr TimestampRange, isRange bool)) []rawMatch {
		var out []rawMatch
		for _, m := range re.FindAllStringSubmatchIndex(q, -1) {
			groups := make([]string, len(m)/2)
			for i := 0; i < len(m)/2; i++ {
				if m[2*i] >= 0 {
					groups[i] = q[m[2*i]:m[2*i+1]]
				}
			}
			tr, isRange := build(groups)
			out = append(out, rawMatch{TimestampRange: tr, idx: m[0], isRange: isRange})
		}
		return out
	}
	var matches []rawMatch
	matches = append(matches, collect(minuteCNRe, func(g []string) (TimestampRange, bool) {
		start := parseCNMinute(g[1])
		end := start
		isRange := false
		if len(g) > 2 && g[2] != "" {
			end = parseCNMinute(g[2])
			isRange = true
		}
		if end < start {
			start, end = end, start
		}
		return TimestampRange{StartMS: start, EndMS: end, Raw: g[0]}, isRange
	})...)
	matches = append(matches, collect(clockRe, func(g []string) (TimestampRange, bool) {
		start := parseClock(g[1], g[2])
		end := start
		isRange := false
		if len(g) > 4 && g[3] != "" && g[4] != "" {
			end = parseClock(g[3], g[4])
			isRange = true
		}
		if end < start {
			start, end = end, start
		}
		return TimestampRange{StartMS: start, EndMS: end, Raw: g[0]}, isRange
	})...)
	matches = append(matches, collect(minSecCNRe, func(g []string) (TimestampRange, bool) {
		start := parseMinSec(g[1], g[2])
		end := start
		isRange := false
		if len(g) > 3 && g[3] != "" {
			end = parseMinSec(g[3], g[4])
			isRange = true
		}
		if end < start {
			start, end = end, start
		}
		return TimestampRange{StartMS: start, EndMS: end, Raw: g[0]}, isRange
	})...)

	// 三组正则会重叠匹配同一段文本（"第15到20分钟" 被 minuteCNRe 整段匹配、又被
	// minSecCNRe 把尾段 "20分钟" 单独匹配）。去重策略：按起始位置排序，区间
	// (isRange) 优先于单点；若两个匹配起点相同，留区间丢单点；若一个匹配的起点
	// 落在已保留区间内，丢弃（被包含）。
	if len(matches) == 0 {
		return nil
	}
	// stable sort by idx, range-first
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			ri, rj := matches[i], matches[j]
			if rj.idx < ri.idx || (rj.idx == ri.idx && rj.isRange && !ri.isRange) {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	var ranges []TimestampRange
	var lastEnd int
	for _, rm := range matches {
		if rm.idx < lastEnd && lastEnd > 0 {
			continue // 落在已保留区间内，丢弃
		}
		rawEnd := rm.idx + len(rm.Raw)
		if rm.idx < lastEnd && rawEnd <= lastEnd {
			continue
		}
		ranges = append(ranges, rm.TimestampRange)
		if rawEnd > lastEnd {
			lastEnd = rawEnd
		}
	}
	return ranges
}

// parseCNMinute: "15" → 15*60*1000 ms。
func parseCNMinute(s string) int64 {
	n := parseInt64(s)
	return n * 60 * 1000
}

// parseClock: "15","00" → 15*60*1000 + 0*1000 ms。
func parseClock(min, sec string) int64 {
	return parseInt64(min)*60*1000 + parseInt64(sec)*1000
}

// parseMinSec: "15","30" → 15*60*1000 + 30*1000 ms。
func parseMinSec(min, sec string) int64 {
	ms := parseInt64(min) * 60 * 1000
	if sec != "" {
		ms += parseInt64(sec) * 1000
	}
	return ms
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// --- 句式 signal 正则 ---
//
// 词表是单一事实源（docs/architecture/retrieval.md review: Duplicated Code）：RuleIntentClassifier 的
// 关键词维度与 Signal 的句式 flag 共用同一份词表，避免两份平行词表手动漂移。
// regex 用 strings.Join(words, "|") 从词表构造，词表改一处两处同步。

var (
	overviewKeywords = []string{
		"讲了什么", "说了什么", "主要内容", "核心内容", "核心观点", "主要观点",
		"视频概括", "视频概览", "总结一下", "简单总结", "简要总结", "简要讲",
		"概括一下", "归纳一下", "overview", "summary", "summarize",
	}
	compareKeywords = []string{
		"对比", "比较", "异同", "区别", "相比", "差异", "不同",
		"哪个好", "哪个更",
	}
	smallTalkKeywords = []string{
		"你好", "您好", "谢谢", "感谢", "在吗", "早安", "晚安",
		"hi", "hello", "hey",
	}

	compareSignalRe   = regexp.MustCompile(strings.Join(compareKeywords, "|"))
	overviewSignalRe  = regexp.MustCompile(strings.Join(overviewKeywords, "|"))
	smallTalkSignalRe = regexp.MustCompile(strings.Join(smallTalkKeywords, "|"))
)

// extractEntities 粗提候选实体（见 docs/architecture/retrieval.md，用于指代消解回指上文实体）。
//
// audit trail：粗提两种最无歧义的形态——引号包裹的名词（用户显式标注的实体）
// 与连续大写英文（专有名词 / 技术术语）。中文专有名词粗提留作后续扩展，本版不
// 做因为中文分词歧义大、无副作用正则难判边界，做了易误提（反 overclaim）。
func extractEntities(q string) []string {
	var entities []string
	seen := make(map[string]struct{})
	// 引号包裹：用户显式标注的实体（"分布式锁" / 'owner'）。
	quoteRe := regexp.MustCompile(`["“']([^"”']{1,40})["”']`)
	for _, m := range quoteRe.FindAllStringSubmatch(q, -1) {
		e := strings.TrimSpace(m[1])
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		entities = append(entities, e)
	}
	// 连续大写英文（≥2 字符）：专有名词 / 技术术语（LLM / DAG）。
	upperRe := regexp.MustCompile(`\b[A-Z]{2,}[A-Za-z0-9]*\b`)
	// 首字母大写词（Redis / Go / React）：专有名词/技术名，≥2 字符避免误提"I"。
	// 与全大写分开匹配，避免全大写词被重复捕获两次。
	capitalRe := regexp.MustCompile(`\b[A-Z][a-z]{2,}[A-Za-z0-9]*\b`)
	for _, m := range upperRe.FindAllStringSubmatch(q, -1) {
		e := strings.TrimSpace(m[0])
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		entities = append(entities, e)
	}
	for _, m := range capitalRe.FindAllStringSubmatch(q, -1) {
		e := strings.TrimSpace(m[0])
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		entities = append(entities, e)
	}
	return entities
}
