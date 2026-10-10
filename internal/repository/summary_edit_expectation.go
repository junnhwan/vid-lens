package repository

import (
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
)

// SummaryEditExpectation fences a selected body before a new operation exists.
// The request hash retains these values for immutable idempotency replay.
type SummaryEditExpectation struct {
	ContentDigest string
	VersionRef    *model.SummaryVersionRef
}

func ValidateSummaryExpectation(current *EffectiveSummary, expected SummaryEditExpectation) error {
	if expected.ContentDigest != "" && expected.ContentDigest != current.ContentDigest {
		return artifact.Err("version_conflict", 409)
	}
	ref := expected.VersionRef
	if ref == nil {
		return nil
	}
	if (ref.RevisionID == "") == (ref.GeneratedVersion == nil) || (ref.GeneratedVersion != nil && *ref.GeneratedVersion < 0) {
		return artifact.Err("invalid_request", 400)
	}
	if ref.RevisionID != "" {
		if current.Revision == nil || current.Revision.ID != ref.RevisionID {
			return artifact.Err("version_conflict", 409)
		}
	} else {
		version := int64(0)
		if current.Generated != nil {
			version = current.Generated.GeneratedVersion
		}
		if current.Revision != nil || version != *ref.GeneratedVersion {
			return artifact.Err("version_conflict", 409)
		}
	}
	return nil
}
