package investigator

import (
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"vidlens/internal/domain"
)

// A question can need several distant source passages at once. Retain each
// actually delivered original slice once, rather than evicting by read count.
// The ordinary input budget still measures the resulting complete context.
func (s *session) projectQuestionSourceReads(messages []*schema.Message) []*schema.Message {
	known := map[string]sourcePassage{}
	for i, segment := range s.transcript {
		known[segment.ID] = sourcePassage{Segment: segment, Alias: fmt.Sprint("t", i+1)}
	}
	seen := map[string]string{}
	out := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role != schema.Tool || message.ToolName != "read_source" {
			out = append(out, message)
			continue
		}
		var body map[string]json.RawMessage
		var page struct {
			SnapshotID string          `json:"snapshot_id"`
			Segments   []sourcePassage `json:"segments"`
		}
		raw := []byte(sourceMessageText(message))
		if json.Unmarshal(raw, &body) != nil || json.Unmarshal(raw, &page) != nil || page.SnapshotID != s.snapshot.ID || body["segments"] == nil {
			out = append(out, message)
			continue
		}
		valid := true
		for _, passage := range page.Segments {
			original, ok := known[passage.ID]
			text := []rune(original.Text)
			if !ok || passage.Alias != original.Alias || passage.TextOffset < 0 || passage.TextEnd < passage.TextOffset || passage.TextEnd > len(text) || passage.StartMS != original.StartMS || passage.EndMS != original.EndMS || passage.Origin != original.Origin || passage.Text != string(text[passage.TextOffset:passage.TextEnd]) {
				valid = false
				break
			}
		}
		if !valid {
			out = append(out, message)
			continue
		}
		retained := []sourcePassage{}
		prior := map[string]string{}
		_ = json.Unmarshal(body["originals_in_prior_results"], &prior)
		if prior == nil {
			prior = map[string]string{}
		}
		for _, passage := range page.Segments {
			key := fmt.Sprintf("%s:%d:%d", passage.Alias, passage.TextOffset, passage.TextEnd)
			if call, ok := seen[key]; ok {
				prior[key] = call
			} else {
				seen[key] = message.ToolCallID
				retained = append(retained, passage)
			}
		}
		body["segments"] = domain.JSON(retained)
		if len(prior) > 0 {
			body["originals_in_prior_results"] = domain.JSON(prior)
		}
		delete(body, "readiness") // Current readiness is rebuilt separately.
		copy := *message
		copy.UserInputMultiContent = nil
		copy.Content = string(domain.JSON(body))
		out = append(out, &copy)
	}
	return out
}
