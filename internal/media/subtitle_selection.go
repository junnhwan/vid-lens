package media

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"vidlens/internal/domain"
)

type subtitleChoice struct {
	Language string `json:"language"`
	Kind     string `json:"kind"`
	Format   string `json:"format"`
}
type subtitleAttempt struct {
	Choice   subtitleChoice `json:"choice"`
	Download string         `json:"download"`
	Parse    string         `json:"parse"`
	Segments int            `json:"segments"`
	Coverage string         `json:"coverage"`
	Usable   bool           `json:"usable"`
}
type subtitleDecision struct {
	DurationMS     int64             `json:"duration_ms,omitempty"`
	CoveredMS      int64             `json:"covered_ms,omitempty"`
	CoverageRule   string            `json:"coverage_rule,omitempty"`
	Revision       string            `json:"revision"`
	Candidates     []subtitleChoice  `json:"candidates"`
	Attempts       []subtitleAttempt `json:"attempts"`
	Selected       *subtitleChoice   `json:"selected,omitempty"`
	Coverage       string            `json:"coverage"`
	Usable         bool              `json:"usable"`
	FallbackReason string            `json:"fallback_reason,omitempty"`
	AdoptedOrigin  string            `json:"adopted_origin,omitempty"`
	Segments       int               `json:"segments"`
}

func subtitleChoices(m metadata) []subtitleChoice {
	out := []subtitleChoice{}
	for _, entry := range []struct {
		tracks map[string][]subformat
		kind   string
	}{{m.Subtitles, "manual"}, {m.Auto, "automatic"}} {
		langs := []string{}
		for lang := range entry.tracks {
			langs = append(langs, lang)
		}
		slices.Sort(langs)
		rank := func(lang string) int {
			for i, want := range []string{"zh-Hans", "zh-CN", "zh", "en", "en-US"} {
				if lang == want {
					return i
				}
			}
			if strings.HasPrefix(lang, "zh") {
				return 5
			}
			if strings.HasPrefix(lang, "en") {
				return 6
			}
			return 10
		}
		slices.SortStableFunc(langs, func(a, b string) int { return rank(a) - rank(b) })
		for _, lang := range langs {
			for _, ext := range []string{"json3", "vtt", "srt", "json"} {
				if slices.ContainsFunc(entry.tracks[lang], func(f subformat) bool { return f.Ext == ext }) {
					out = append(out, subtitleChoice{Language: lang, Kind: entry.kind, Format: ext})
					break
				}
			}
		}
	}
	return out
}

func (s *Service) extractorSubtitles(ctx context.Context, c domain.Claim, m metadata, infoFile, work string) ([]domain.Segment, *string, subtitleDecision, error) {
	d := subtitleDecision{Revision: "subtitle-adoption-v2", Candidates: subtitleChoices(m), Attempts: []subtitleAttempt{}, FallbackReason: "no_supported_track"}
	if err := s.Store.SourceDiagnostic(ctx, c, "subtitle-selection", d); err != nil {
		return nil, nil, d, err
	}
	for i, choice := range d.Candidates[:min(3, len(d.Candidates))] {
		// Use the exact metadata that supplied the track. A second extraction can
		// select a different client/track. Isolate quotas from retained video bytes
		// in the run workspace after recovery.
		dir := filepath.Join(work, "subtitle-"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, nil, d, err
		}
		a := append(s.ytdlpArgs("https://www.youtube.com/"), "--skip-download", "--load-info-json", infoFile, "--sub-langs", regexp.QuoteMeta(choice.Language), "--sub-format", choice.Format, "-o", filepath.Join(dir, "sub.%(ext)s"))
		if choice.Kind == "automatic" {
			a = append(a, "--write-auto-subs")
		} else {
			a = append(a, "--write-subs")
		}
		dctx, release, err := s.Resources.Acquire(ctx, c, "download")
		if err != nil {
			return nil, nil, d, err
		}
		sub, cancel := context.WithTimeout(dctx, 60*time.Second)
		_, err = command(sub, "yt-dlp", a, dir, 16<<20, 0)
		cancel()
		release()
		if ctx.Err() != nil {
			return nil, nil, d, ctx.Err()
		}
		attempt := subtitleAttempt{Choice: choice, Download: "failed", Parse: "not_attempted", Coverage: "unknown"}
		var parsed []domain.Segment
		if err == nil {
			attempt.Download = "no_file"
			files, _ := filepath.Glob(filepath.Join(dir, "sub.*"))
			for _, file := range files {
				if filepath.Ext(file) != "."+choice.Format {
					continue
				}
				b, e := os.ReadFile(file)
				if e != nil || len(b) > 16<<20 {
					continue
				}
				attempt.Download = "received"
				attempt.Parse = "invalid"
				parsed, e = ParseSubtitles(b, filepath.Ext(file))
				if e == nil && len(parsed) > 0 {
					attempt.Parse = "parsed"
					break
				}
			}
		}
		attempt.Segments = len(parsed)
		attempt.Usable, attempt.Coverage = TranscriptCoverage(parsed, int64(m.Duration*1000))
		d.Attempts = append(d.Attempts, attempt)
		if err := s.Store.SourceDiagnostic(ctx, c, "subtitle-attempt-"+strconv.Itoa(i), attempt); err != nil {
			return nil, nil, d, err
		}
		if attempt.Usable {
			d.Selected = &choice
			d.Coverage = attempt.Coverage
			d.Usable = true
			d.FallbackReason = ""
			return parsed, &choice.Language, d, nil
		}
		d.FallbackReason = "coverage_below_threshold"
		if attempt.Parse != "parsed" {
			d.FallbackReason = "parse_or_format_failed"
		}
		if attempt.Download != "received" {
			d.FallbackReason = "download_" + attempt.Download
		}
	}
	return nil, nil, d, nil
}
