// Package summarydoc defines the authoritative summary-v2 content contract.
// It is independent of persistence, model calls, and resource authorization.
package summarydoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

const (
	SchemaVersion    = "summary-v2"
	HashKind         = "summary-json-v2"
	MaxDocumentBytes = 2 << 20
)

type Document struct {
	SchemaVersion    string  `json:"schema_version"`
	DocumentID       string  `json:"document_id"`
	SourceID         string  `json:"source_id"`
	SourceDigest     string  `json:"source_digest"`
	MediaRevision    string  `json:"media_revision"`
	PresentationMode string  `json:"presentation_mode"`
	Title            string  `json:"title"`
	Overview         string  `json:"overview"`
	Blocks           []Block `json:"blocks"`
}

type Block struct {
	ID           string      `json:"id"`
	ParentID     *string     `json:"parent_id"`
	Order        int         `json:"order"`
	Title        string      `json:"title"`
	BodyMarkdown string      `json:"body_markdown"`
	SourceRefs   []SourceRef `json:"source_refs"`
	Figures      []Figure    `json:"figures"`
}

type SourceRef struct {
	SourceID     string   `json:"source_id"`
	CueIDs       []string `json:"cue_ids"`
	StartMS      *int64   `json:"start_ms"`
	EndMS        *int64   `json:"end_ms"`
	TimingMethod string   `json:"timing_method"`
}

type Figure struct {
	ID            string `json:"id"`
	ScreenshotRef string `json:"screenshot_ref"`
	CaptureMS     *int64 `json:"capture_ms"`
	Caption       string `json:"caption"`
	Alt           string `json:"alt"`
	Supports      string `json:"supports"`
}

// Cue contains only server-frozen source facts. Missing time is null/unknown.
type Cue struct {
	StartMS      *int64
	EndMS        *int64
	TimingMethod string
}

// RegisteredFigure is supplied by the authorized resource resolver, never by
// model output. Inspected records actual successful visual inspection.
type RegisteredFigure struct {
	SourceID      string
	SourceDigest  string
	MediaRevision string
	BlockID       string
	GenerationID  string
	CaptureMS     int64
	Inspected     bool
}

type ValidationContext struct {
	SourceID      string
	SourceDigest  string
	MediaRevision string
	GenerationID  string
	Cues          map[string]Cue
	Figures       map[string]RegisteredFigure
}

// Parse rejects unknown fields, duplicate object keys, invalid UTF-8, excessive
// input, and anything after the single JSON value. Call Validate before publish.
func Parse(data []byte) (Document, error) {
	var doc Document
	if err := strictDecode(data, &doc); err != nil {
		return doc, err
	}
	if err := validateStructure(doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func strictDecode(data []byte, dst any) error {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return fmt.Errorf("summary document size exceeds limit")
	}
	if !validUTF8(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueKeys(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid summary JSON: %w", err)
	}
	return nil
}

func uniqueKeys(dec *json.Decoder, depth int) error {
	if depth > 24 {
		return fmt.Errorf("JSON nesting exceeds limit")
	}
	t, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		keys := map[string]bool{}
		for dec.More() {
			key, e := dec.Token()
			if e != nil {
				return e
			}
			s, ok := key.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if keys[s] {
				return fmt.Errorf("duplicate JSON key %q", s)
			}
			keys[s] = true
			if e = uniqueKeys(dec, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for dec.More() {
			if err := uniqueKeys(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = dec.Token()
	return err
}

// CanonicalJSON fixes field and collection ordering without mutating input.
// No access URL, fetch timestamp, or outer revision field exists in this type.
func CanonicalJSON(doc Document) ([]byte, error) {
	if err := validateStructure(doc); err != nil {
		return nil, err
	}
	doc = clone(doc)
	doc.Blocks = OrderedBlocks(doc)
	if doc.Blocks == nil {
		doc.Blocks = []Block{}
	}
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		if b.SourceRefs == nil {
			b.SourceRefs = []SourceRef{}
		}
		if b.Figures == nil {
			b.Figures = []Figure{}
		}
		for j := range b.SourceRefs {
			sort.Strings(b.SourceRefs[j].CueIDs)
		}
		sort.Slice(b.SourceRefs, func(i, j int) bool {
			a, _ := json.Marshal(b.SourceRefs[i])
			c, _ := json.Marshal(b.SourceRefs[j])
			return bytes.Compare(a, c) < 0
		})
		sort.Slice(b.Figures, func(i, j int) bool { return b.Figures[i].ID < b.Figures[j].ID })
	}
	return json.Marshal(doc)
}

func Digest(doc Document) (string, error) {
	data, err := CanonicalJSON(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// OrderedBlocks returns a stable depth-first display order (order, then ID).
func OrderedBlocks(doc Document) []Block {
	children := make(map[string][]Block)
	for _, block := range doc.Blocks {
		p := ""
		if block.ParentID != nil {
			p = *block.ParentID
		}
		children[p] = append(children[p], block)
	}
	for p := range children {
		sort.Slice(children[p], func(i, j int) bool {
			a, b := children[p][i], children[p][j]
			if a.Order != b.Order {
				return a.Order < b.Order
			}
			return a.ID < b.ID
		})
	}
	var out []Block
	var visit func(string)
	visit = func(p string) {
		for _, b := range children[p] {
			out = append(out, b)
			visit(b.ID)
		}
	}
	visit("")
	return out
}

func clone(doc Document) Document {
	data, _ := json.Marshal(doc)
	var out Document
	_ = json.Unmarshal(data, &out)
	return out
}
