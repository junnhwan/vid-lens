package domain

import "strings"

const PurposeCharsMax = 500

type GenerationModel struct {
	ID              string `json:"id"`
	RevisionID      string `json:"revision_id"`
	DisplayName     string `json:"display_name"`
	BrandID         string `json:"brand_id"`
	Description     string `json:"description,omitempty"`
	SelectionLocked bool   `json:"selection_locked"`
	Legacy          bool   `json:"legacy,omitempty"`
}
type GenerationSpec struct {
	Version  int             `json:"version"`
	Purpose  string          `json:"purpose"`
	Scope    string          `json:"scope"`
	ConfigID string          `json:"config_id"`
	Model    GenerationModel `json:"model"`
}
type RequestInput struct {
	VideoURL        string `json:"video_url"`
	Purpose         string `json:"purpose"`
	ModelID         string `json:"model_id"`
	ModelRevisionID string `json:"model_revision_id"`
}

func NormalizePurpose(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
}
