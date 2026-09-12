package service

import "encoding/json"

// These schemas describe the planner input, not runtime dependencies such as
// user/task ownership or provider credentials. Citations are resolved back to
// observed evidence by canonicalizeResearchAnswerArguments before execution.
func videoAgentToolInputSchema(name string) json.RawMessage {
	switch name {
	case VideoAgentToolSearchTranscript, VideoAgentToolSearchVisualEvidence:
		return json.RawMessage(`{"type":"object","properties":{
			"question":{"type":"string","minLength":1,"description":"检索问题或关键词"},
			"top_k":{"type":"integer","minimum":1,"description":"可选，省略时使用当前运行默认值"}
		},"required":["question"],"additionalProperties":false}`)
	case VideoAgentToolGetTranscriptWindow:
		return json.RawMessage(`{"type":"object","properties":{
			"chunk_index":{"type":"integer","minimum":0,"description":"已命中转写片段的 chunk_index"},
			"radius":{"type":"integer","minimum":0,"description":"向前后扩展的片段数，可选"}
		},"required":["chunk_index"],"additionalProperties":false}`)
	case VideoAgentToolInspectVisualWindow:
		return json.RawMessage(`{"type":"object","properties":{
			"start_ms":{"type":"integer","minimum":0},
			"end_ms":{"type":"integer","minimum":0,"description":"不小于 start_ms，窗口最多 600000 毫秒"},
			"max_frames":{"type":"integer","minimum":1,"maximum":8}
		},"required":["start_ms","end_ms"],"additionalProperties":false}`)
	case VideoAgentToolInvestigateVisual:
		return json.RawMessage(`{"type":"object","properties":{
			"goal":{"type":"string","minLength":1},
			"required_facts":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}},
			"seed_windows":{"type":"array","minItems":1,"items":{"type":"object","properties":{"start_ms":{"type":"integer","minimum":0},"end_ms":{"type":"integer","minimum":0}},"required":["start_ms","end_ms"],"additionalProperties":false}},
			"budget":{"type":"object","description":"可省略，使用服务端硬预算；不能扩大运行授权范围","properties":{
				"max_windows":{"type":"integer","minimum":1},"max_frames":{"type":"integer","minimum":1},
				"max_vlm_calls":{"type":"integer","minimum":1},"max_window_ms":{"type":"integer","minimum":1},"max_total_ms":{"type":"integer","minimum":1}
			},"additionalProperties":false}
		},"required":["goal","required_facts","seed_windows"],"additionalProperties":false}`)
	case VideoAgentToolBuildCitedAnswer:
		return json.RawMessage(`{"type":"object","properties":{
			"question":{"type":"string","minLength":1},
			"intermediate":{"type":"string","description":"已有发现、需要说明的证据缺口与不确定性"},
			"citations":{"type":"array","description":"仅选择当前 state.evidence 中的标识，正文由服务端恢复；无证据时传空数组","items":{
				"type":"object","properties":{"evidence_id":{"type":"string"},"task_id":{"type":"integer"},"chunk_id":{"type":"integer"}},
				"anyOf":[{"required":["evidence_id"]},{"required":["task_id","chunk_id"]}],"additionalProperties":false
			}}
		},"required":["question","intermediate","citations"],"additionalProperties":false}`)
	default:
		return nil
	}
}
