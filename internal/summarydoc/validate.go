package summarydoc

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var rawHTML = regexp.MustCompile(`(?i)<\s*(?:!|/?[a-z][a-z0-9:-]*(?:\s|/?>))`)
var markdownLink = regexp.MustCompile(`(?s)\]\(\s*<?([^\s)>]+)`)
var referenceLink = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]\n]+\]:\s*<?([^\s>]+)`)
var embeddedURL = regexp.MustCompile(`(?i)https?://[^\s<>]+`)

func validUTF8(data []byte) bool { return utf8.Valid(data) }
func validID(id string) bool     { return identifier.MatchString(id) }

// Source cues are opaque source-owned keys, including ASR window:rune:rune
// provenance. Keep their byte limit aligned with textsource; authorization is
// exact membership in the caller's frozen cue registry, not this syntax check.
func validCueID(id string) bool {
	if id == "" || len(id) > 128 || !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func reservedID(id string) bool {
	return id == "title" || id == "overview" || id == "summary-title" || id == "summary-overview"
}

func bounded(value string, max int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func validateStructure(doc Document) error {
	if doc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version")
	}
	if !validID(doc.DocumentID) || !validID(doc.SourceID) || !validID(doc.SourceDigest) || !validID(doc.MediaRevision) {
		return fmt.Errorf("invalid document/source identity")
	}
	if strings.TrimSpace(doc.Title) == "" || !bounded(doc.Title, 300) || !bounded(doc.Overview, 12000) {
		return fmt.Errorf("invalid document title or overview")
	}
	if err := safeMarkdown(doc.Overview); err != nil {
		return fmt.Errorf("overview: %w", err)
	}
	if err := rejectPrivateURLs(doc.Title); err != nil {
		return err
	}
	if len(doc.Blocks) > 512 {
		return fmt.Errorf("too many blocks")
	}
	ids := map[string]bool{}
	blocks := map[string]Block{}
	figures, readable, totalRunes := 0, strings.TrimSpace(doc.Overview) != "", utf8.RuneCountInString(doc.Overview)
	for _, b := range doc.Blocks {
		if !validID(b.ID) || reservedID(b.ID) || ids[b.ID] {
			return fmt.Errorf("invalid, reserved, or duplicate block ID %q", b.ID)
		}
		ids[b.ID] = true
		blocks[b.ID] = b
		if b.ParentID != nil && !validID(*b.ParentID) {
			return fmt.Errorf("invalid parent_id for %s", b.ID)
		}
		if b.Order < 0 || b.Order > 1000000 {
			return fmt.Errorf("invalid order for %s", b.ID)
		}
		if !bounded(b.Title, 300) || !bounded(b.BodyMarkdown, 48000) {
			return fmt.Errorf("block text exceeds limit for %s", b.ID)
		}
		if err := safeMarkdown(b.BodyMarkdown); err != nil {
			return fmt.Errorf("block %s: %w", b.ID, err)
		}
		if err := rejectPrivateURLs(b.Title); err != nil {
			return err
		}
		readable = readable || strings.TrimSpace(b.BodyMarkdown) != ""
		totalRunes += utf8.RuneCountInString(b.Title) + utf8.RuneCountInString(b.BodyMarkdown)
		if len(b.SourceRefs) > 128 || len(b.Figures) > 32 {
			return fmt.Errorf("too many references for %s", b.ID)
		}
		for _, ref := range b.SourceRefs {
			if ref.SourceID != doc.SourceID || len(ref.CueIDs) < 1 || len(ref.CueIDs) > 512 {
				return fmt.Errorf("invalid source reference for %s", b.ID)
			}
			seen := map[string]bool{}
			for _, id := range ref.CueIDs {
				if !validCueID(id) || seen[id] {
					return fmt.Errorf("invalid/duplicate cue %q", id)
				}
				seen[id] = true
			}
			if ref.TimingMethod == "unknown" {
				if ref.StartMS != nil || ref.EndMS != nil {
					return fmt.Errorf("unknown timing must have null timestamps")
				}
			} else if ref.TimingMethod == "" || ref.StartMS == nil || ref.EndMS == nil || *ref.StartMS < 0 || *ref.EndMS <= *ref.StartMS {
				return fmt.Errorf("invalid source timing")
			}
		}
		for _, f := range b.Figures {
			if !validID(f.ID) || reservedID(f.ID) || ids[f.ID] {
				return fmt.Errorf("invalid or duplicate figure ID %q", f.ID)
			}
			ids[f.ID] = true
			if !validID(f.ScreenshotRef) || f.CaptureMS == nil || *f.CaptureMS < 0 {
				return fmt.Errorf("invalid figure resource/time")
			}
			if strings.TrimSpace(f.Caption) == "" || strings.TrimSpace(f.Alt) == "" || !bounded(f.Caption, 2000) || !bounded(f.Alt, 2000) || !bounded(f.Supports, 2000) {
				return fmt.Errorf("invalid figure caption/alt")
			}
			for _, text := range []string{f.Caption, f.Alt, f.Supports} {
				if err := rejectPrivateURLs(text); err != nil {
					return err
				}
			}
			totalRunes += utf8.RuneCountInString(f.Caption) + utf8.RuneCountInString(f.Alt) + utf8.RuneCountInString(f.Supports)
			figures++
		}
	}
	if totalRunes > 200000 || figures > 128 {
		return fmt.Errorf("document content exceeds limit")
	}
	// IDs are shared across blocks and figures, even when the figure occurs first.
	if len(ids) != len(blocks)+figures {
		return fmt.Errorf("duplicate block/figure IDs")
	}
	for _, b := range doc.Blocks {
		depth := 0
		seen := map[string]bool{b.ID: true}
		p := b.ParentID
		for p != nil {
			parent, exists := blocks[*p]
			if !exists {
				return fmt.Errorf("missing parent %q", *p)
			}
			if seen[*p] {
				return fmt.Errorf("block parent cycle")
			}
			seen[*p] = true
			depth++
			if depth > 12 {
				return fmt.Errorf("block hierarchy exceeds depth limit")
			}
			p = parent.ParentID
		}
	}
	switch doc.PresentationMode {
	case "text":
		if !readable || figures != 0 {
			return fmt.Errorf("text mode requires readable text and no figures")
		}
	case "image_text":
		if !readable || figures == 0 {
			return fmt.Errorf("image_text requires readable text and an inspected figure")
		}
	case "keyframes":
		if figures == 0 {
			return fmt.Errorf("keyframes requires an inspected figure")
		}
	default:
		return fmt.Errorf("invalid presentation_mode")
	}
	return nil
}

// Validate verifies every reference against server-owned frozen source/resource
// facts. The caller must build the context only after owner/task authorization.
func Validate(doc Document, ctx ValidationContext) error {
	if err := validateStructure(doc); err != nil {
		return err
	}
	if ctx.SourceID == "" || ctx.SourceDigest == "" || ctx.MediaRevision == "" || doc.SourceID != ctx.SourceID || doc.SourceDigest != ctx.SourceDigest || doc.MediaRevision != ctx.MediaRevision {
		return fmt.Errorf("frozen source identity mismatch")
	}
	for _, b := range doc.Blocks {
		for _, ref := range b.SourceRefs {
			var start, end int64
			method := ""
			unknown := false
			timed := false
			for i, id := range ref.CueIDs {
				cue, ok := ctx.Cues[id]
				if !ok {
					return fmt.Errorf("unknown source cue %q", id)
				}
				if cue.StartMS == nil || cue.EndMS == nil || cue.TimingMethod == "unknown" {
					if cue.StartMS != nil || cue.EndMS != nil || cue.TimingMethod != "unknown" {
						return fmt.Errorf("invalid frozen cue timing %q", id)
					}
					unknown = true
					continue
				}
				if cue.TimingMethod == "" || *cue.StartMS < 0 || *cue.EndMS <= *cue.StartMS {
					return fmt.Errorf("invalid frozen cue timing %q", id)
				}
				timed = true
				if method != "" && method != cue.TimingMethod {
					return fmt.Errorf("mixed cue timing methods require separate source references")
				}
				if method == "" {
					method = cue.TimingMethod
				}
				if i == 0 || *cue.StartMS < start {
					start = *cue.StartMS
				}
				if i == 0 || *cue.EndMS > end {
					end = *cue.EndMS
				}
			}
			if timed && unknown {
				return fmt.Errorf("timed and unknown cues require separate source references")
			}
			if unknown {
				if ref.TimingMethod != "unknown" || ref.StartMS != nil || ref.EndMS != nil {
					return fmt.Errorf("cue timing is unknown; cannot invent mapped time")
				}
			} else if ref.TimingMethod != method || ref.StartMS == nil || ref.EndMS == nil || *ref.StartMS != start || *ref.EndMS != end {
				return fmt.Errorf("source reference timing differs from frozen cues")
			}
		}
		for _, f := range b.Figures {
			reg, ok := ctx.Figures[f.ScreenshotRef]
			if !ok || !reg.Inspected || reg.SourceID != doc.SourceID || reg.SourceDigest != doc.SourceDigest || reg.MediaRevision != doc.MediaRevision || reg.BlockID != b.ID || ctx.GenerationID == "" || reg.GenerationID != ctx.GenerationID || reg.CaptureMS != *f.CaptureMS {
				return fmt.Errorf("figure %s is not an inspected authorized resource for this source/block/generation", f.ID)
			}
		}
	}
	return nil
}

// Body media must use controlled Figure references. Raw HTML and executable or
// credential-bearing links are rejected before either rendering or export.
func safeMarkdown(text string) error {
	decoded := html.UnescapeString(text)
	if err := rejectPrivateURLs(decoded); err != nil {
		return err
	}
	decoded = withoutCode(decoded)
	if rawHTML.MatchString(decoded) || strings.Contains(decoded, "![") {
		return fmt.Errorf("body HTML/images must use controlled figures")
	}
	for _, re := range []*regexp.Regexp{markdownLink, referenceLink} {
		for _, m := range re.FindAllStringSubmatch(decoded, -1) {
			target := strings.ReplaceAll(m[1], "\\", "")
			if strings.HasPrefix(target, "#") {
				continue
			}
			u, err := url.Parse(target)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
				return fmt.Errorf("unsafe Markdown link")
			}
			for key := range u.Query() {
				k := strings.ToLower(key)
				if strings.Contains(k, "signature") || strings.Contains(k, "token") || strings.Contains(k, "credential") || strings.Contains(k, "secret") || strings.HasPrefix(k, "x-amz-") {
					return fmt.Errorf("private access URL cannot be document content")
				}
			}
		}
	}
	return nil
}

// Literal code is rendered by the existing Markdown renderer as escaped text.
// Recognize fenced and inline code so useful HTML/Markdown examples remain valid.
func withoutCode(text string) string {
	var out strings.Builder
	var marker byte
	fenceLen := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if fenceLen == 0 && n >= 3 && (trimmed[0] != '`' || !strings.Contains(trimmed[n:], "`")) {
				marker = trimmed[0]
				fenceLen = n
				out.WriteByte('\n')
				continue
			}
			if fenceLen > 0 && trimmed[0] == marker && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
				fenceLen = 0
				out.WriteByte('\n')
				continue
			}
		}
		if fenceLen == 0 {
			out.WriteString(line)
		} else {
			out.WriteByte('\n')
		}
	}
	text = out.String()
	out.Reset()
	for i := 0; i < len(text); {
		if text[i] != '`' {
			out.WriteByte(text[i])
			i++
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			out.WriteByte(text[i])
			i++
			continue
		}
		n := 1
		for i+n < len(text) && text[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(text); {
			if text[j] != '`' {
				j++
				continue
			}
			k := 1
			for j+k < len(text) && text[j+k] == '`' {
				k++
			}
			if k == n {
				end = j + k
				break
			}
			j += k
		}
		if end < 0 {
			out.WriteString(text[i : i+n])
			i += n
		} else {
			out.WriteByte(' ')
			i = end
		}
	}
	return out.String()
}

func rejectPrivateURLs(text string) error {
	for _, target := range embeddedURL.FindAllString(html.UnescapeString(text), -1) {
		u, err := url.Parse(target)
		if err != nil {
			return fmt.Errorf("invalid document URL")
		}
		if u.User != nil {
			return fmt.Errorf("credential-bearing URL cannot be document content")
		}
		for key := range u.Query() {
			k := strings.ToLower(key)
			if strings.Contains(k, "signature") || strings.Contains(k, "token") || strings.Contains(k, "credential") || strings.Contains(k, "secret") || strings.HasPrefix(k, "x-amz-") {
				return fmt.Errorf("private access URL cannot be document content")
			}
		}
	}
	return nil
}
