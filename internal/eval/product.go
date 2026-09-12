package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ProductSchemaVersion = "vidlens.product.v1"

// ProductCase is a sequence, not a bag of independent turns. Session IDs must
// refer to explicitly provisioned evaluation sessions; the harness never resets data.
type ProductCase struct {
	Version           string        `json:"version"`
	ID                string        `json:"case_id"`
	SourceGroup       string        `json:"source_group"`
	Family            string        `json:"family"`
	Split             string        `json:"split"`
	AssetVersion      string        `json:"asset_version"`
	AnnotationVersion string        `json:"annotation_version"`
	ReviewStatus      string        `json:"review_status"`
	ReviewedBy        string        `json:"reviewed_by,omitempty"`
	ReviewedDate      string        `json:"reviewed_date,omitempty"`
	SourceChecked     bool          `json:"source_checked,omitempty"`
	ReviewNote        string        `json:"review_note,omitempty"`
	RequiredPoints    []string      `json:"required_points,omitempty"`
	EvidenceGroups    [][]string    `json:"evidence_groups,omitempty"`
	Turns             []ProductTurn `json:"turns"`
}

type ProductTurn struct {
	UserID    int64  `json:"user_id"`
	SessionID int64  `json:"session_id"`
	Kind      string `json:"kind"`
	Question  string `json:"question"`
	TopK      int    `json:"top_k,omitempty"`
	Stream    bool   `json:"stream"`
	RunID     string `json:"run_id,omitempty"`
}

type ProductUsage struct {
	Source           string `json:"source"`
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
}

type ProductObservation struct {
	SessionID       int64           `json:"session_id"`
	RunID           string          `json:"run_id,omitempty"`
	MessageID       int64           `json:"message_id"`
	Answer          string          `json:"answer"`
	Model           string          `json:"model,omitempty"`
	Status          string          `json:"status"`
	StopReason      string          `json:"stop_reason,omitempty"`
	Degraded        bool            `json:"degraded"`
	Citations       json.RawMessage `json:"citations,omitempty"`
	Usage           ProductUsage    `json:"usage"`
	DurationMS      float64         `json:"duration_ms"`
	FirstProgressMS *float64        `json:"first_progress_ms"`
	FirstAnswerMS   *float64        `json:"first_answer_ms"`
	Error           string          `json:"error,omitempty"`
	Classification  string          `json:"classification"`
}

type ProductResult struct {
	Version        string               `json:"version"`
	CaseID         string               `json:"case_id"`
	SourceGroup    string               `json:"source_group"`
	Family         string               `json:"family"`
	Classification string               `json:"classification"`
	Turns          []ProductObservation `json:"turns"`
	// SemanticSuccess stays unknown until evidence-backed human review. A saved
	// answer, citation or done event is not a correctness label.
	SemanticSuccess *bool `json:"semantic_success"`
}

type ProductExecutor func(context.Context, ProductTurn) (ProductObservation, error)

func ValidateProductCases(cases []ProductCase) error {
	seen := map[string]bool{}
	groups := map[string]string{}
	for _, c := range cases {
		if c.Version != ProductSchemaVersion || c.ID == "" || c.SourceGroup == "" || len(c.Turns) == 0 || seen[c.ID] {
			return fmt.Errorf("invalid product case %q", c.ID)
		}
		if c.Split != "dev" {
			return fmt.Errorf("case %q: product runner only permits dev; sealed test requires separate release protocol", c.ID)
		}
		if prior, ok := groups[c.SourceGroup]; ok && prior != c.Split {
			return fmt.Errorf("source group crosses splits: %s", c.SourceGroup)
		}
		seen[c.ID], groups[c.SourceGroup] = true, c.Split
		for _, turn := range c.Turns {
			if turn.SessionID <= 0 || turn.Question == "" || (turn.Kind != "chat" && turn.Kind != "agent") {
				return fmt.Errorf("invalid turn in %q", c.ID)
			}
		}
	}
	return nil
}

// RunProductCases retains every scheduled case, including failures and cases
// whose context is already cancelled. Later turns stop after a failed dependency.
func RunProductCases(ctx context.Context, cases []ProductCase, execute ProductExecutor) ([]ProductResult, error) {
	if err := ValidateProductCases(cases); err != nil {
		return nil, err
	}
	if execute == nil {
		return nil, errors.New("product executor required")
	}
	results := make([]ProductResult, 0, len(cases))
	for _, c := range cases {
		r := ProductResult{Version: ProductSchemaVersion, CaseID: c.ID, SourceGroup: c.SourceGroup, Family: c.Family, Classification: "completed"}
		for _, turn := range c.Turns {
			start := time.Now()
			ob, err := execute(ctx, turn)
			ob.DurationMS = float64(time.Since(start).Microseconds()) / 1000
			ob.SessionID = turn.SessionID
			if ob.Usage.Source == "" {
				ob.Usage.Source = "unknown"
			}
			if ob.Usage.Source == "unknown" {
				ob.Usage.PromptTokens, ob.Usage.CompletionTokens = nil, nil
			}
			ob.Classification = ClassifyProductObservation(ob, err)
			if err != nil {
				ob.Error = err.Error()
			}
			r.Turns = append(r.Turns, ob)
			if ob.Classification != "completed" {
				r.Classification = ob.Classification
			}
			if ob.Classification == "failed" || ob.Classification == "cancelled" || ob.Classification == "pending_confirmation" {
				break
			}
		}
		results = append(results, r)
	}
	return results, nil
}

func ClassifyProductObservation(ob ProductObservation, err error) string {
	if errors.Is(err, context.Canceled) || ob.Status == "cancelled" {
		return "cancelled"
	}
	if err != nil || ob.Status == "failed" {
		return "failed"
	}
	if ob.MessageID <= 0 || ob.Answer == "" {
		return "pending_confirmation"
	}
	if ob.Degraded || ob.Status == "budget_exhausted" {
		return "limited"
	}
	if ob.Status != "completed" {
		return "pending_confirmation"
	}
	return "completed"
}

func ProductDistribution(results []ProductResult) map[string]int {
	counts := map[string]int{"total": len(results), "completed": 0, "limited": 0, "failed": 0, "cancelled": 0, "pending_confirmation": 0}
	for _, r := range results {
		counts[r.Classification]++
	}
	return counts
}
