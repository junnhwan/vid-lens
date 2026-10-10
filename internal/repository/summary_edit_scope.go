package repository

import (
	"encoding/json"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/summarydoc"
	"vid-lens/internal/summaryselection"
)

func ValidateSummaryEditScope(op *model.SummaryEditOperation, raw string) error {
	if op.SelectedBlockIDsJSON == "" || op.SelectedBlockIDsJSON == "[]" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(op.SelectedBlockIDsJSON), &ids); err != nil {
		return artifact.Err("invalid_edit_scope", 422)
	}
	var err error
	if op.BaseDocumentJSON != "" {
		base, e := summarydoc.Parse([]byte(op.BaseDocumentJSON))
		if e != nil {
			return artifact.Err("invalid_document", 422)
		}
		patch, e := summarydoc.ParsePatch([]byte(raw))
		if e != nil {
			return artifact.Err("invalid_patch", 422)
		}
		err = summaryselection.ValidateScope(base, ids, patch)
	} else {
		err = summaryselection.ValidateLegacyScope(op.BaseContent, ids, raw)
	}
	if err != nil {
		return artifact.Err("edit_scope_violation", 422)
	}
	return nil
}
