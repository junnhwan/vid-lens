package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

func (r VideoAgentToolRuntime) checkScope(ctx context.Context, evidence []RetrievedChunk) error {
	if r.ValidateScope != nil {
		if err := r.ValidateScope(ctx); err != nil {
			return err
		}
	}
	if len(r.TaskIDs) == 0 {
		return validateObservedResearchEvidence(r.TaskID, evidence)
	}
	allowed := make(map[int64]bool, len(r.TaskIDs))
	for _, id := range r.TaskIDs {
		allowed[id] = true
	}
	for _, item := range evidence {
		if !allowed[item.TaskID] {
			return fmt.Errorf("证据超出知识库成员范围: %d", item.TaskID)
		}
	}
	return nil
}

func sameTaskIDs(a, b []int64) bool {
	a, b = normalizeTaskIDs(a), normalizeTaskIDs(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Collection schemas explicitly require a video for local windows, while
// search can cover the collection. Membership always comes from the server.
func (r *VideoAgentToolRegistry) useCollectionSchemas() {
	for name, tool := range r.tools {
		if name == VideoAgentToolBuildCitedAnswer {
			continue
		}
		def := tool.Definition()
		var schema map[string]any
		if json.Unmarshal(def.InputSchema, &schema) != nil {
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		props["task_id"] = map[string]any{"type": "integer", "minimum": 1, "description": "从已观察证据中选择视频 task_id；检索时可省略以搜索整个知识库"}
		if name != VideoAgentToolSearchTranscript && name != VideoAgentToolSearchVisualEvidence {
			required, _ := schema["required"].([]any)
			schema["required"] = append(required, "task_id")
		}
		def.InputSchema, _ = json.Marshal(schema)
		def.Description = "在授权知识库中执行；窗口操作的时间属于指定视频。" + def.Description
		r.tools[name] = &definedVideoAgentTool{definition: def, implementation: tool}
	}
}

var errKnowledgeMembershipChanged = errors.New("知识库成员或索引已变化，请重新发起研究")
