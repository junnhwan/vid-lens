package investigator

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// Preserve the original response in its model operation. Only a validation
// projection may remove unknown null annotations or exact, identity-bound echoes
// of host input. Unknown nonempty meanings, required fields and conflicts stay
// under the existing strict decoder and explicit coverage/evidence validator.
func projectReviewContextEcho(response *schema.Message, task reviewTask, input []*schema.Message) (*schema.Message, []string) {
	if response == nil || len(response.ToolCalls) != 1 || response.ToolCalls[0].Function.Name != reviewUnitsTool {
		return response, nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(response.ToolCalls[0].Function.Arguments), &root) != nil || root == nil {
		return response, nil
	}
	context := map[string]json.RawMessage{}
	for _, message := range input {
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(message.Content), &fields) == nil && fields["request_id"] != nil && fields["units"] != nil {
			context = fields
			break
		}
	}
	ignored := projectReviewFields(root, []string{"request_id", "results", "activity_title"}, context, "")
	units := map[string]reviewUnit{}
	for _, unit := range task.Units {
		units[unit.ID] = unit
	}
	var results []json.RawMessage
	if json.Unmarshal(root["results"], &results) == nil {
		for i, raw := range results {
			var result map[string]json.RawMessage
			if json.Unmarshal(raw, &result) != nil || result == nil {
				continue
			}
			var id string
			_ = json.Unmarshal(result["unit_id"], &id)
			echo := map[string]json.RawMessage{}
			if unit, ok := units[id]; ok {
				_ = json.Unmarshal(domain.JSON(unit), &echo)
			}
			prefix := "results/" + strconv.Itoa(i) + "/"
			ignored = append(ignored, projectReviewFields(result, []string{"unit_id", "verdict", "issues"}, echo, prefix)...)
			var issues []map[string]json.RawMessage
			if json.Unmarshal(result["issues"], &issues) == nil {
				for j, issue := range issues {
					ignored = append(ignored, projectReviewFields(issue, []string{"claim", "reason", "citation_ids"}, echo, prefix+"issues/"+strconv.Itoa(j)+"/")...)
				}
				result["issues"] = domain.JSON(issues)
			}
			results[i] = domain.JSON(result)
		}
		root["results"] = domain.JSON(results)
	}
	if len(ignored) == 0 {
		return response, nil
	}
	slices.Sort(ignored)
	projected := *response
	projected.ToolCalls = append([]schema.ToolCall(nil), response.ToolCalls...)
	projected.ToolCalls[0].Function.Arguments = string(domain.JSON(root))
	return &projected, ignored
}

func projectReviewFields(fields map[string]json.RawMessage, required []string, context map[string]json.RawMessage, prefix string) []string {
	removed := []string{}
	for key, value := range fields {
		if slices.Contains(required, key) {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || context[key] != nil && equalReviewJSON(value, context[key]) {
			delete(fields, key)
			removed = append(removed, prefix+key)
		}
	}
	return removed
}

func equalReviewJSON(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, error) {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&value)
		return value, err
	}
	a, e := decode(left)
	b, f := decode(right)
	return e == nil && f == nil && reflect.DeepEqual(a, b)
}
