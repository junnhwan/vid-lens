package media

import (
	"encoding/json"
	"errors"
	"html"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"vidlens/internal/domain"
)

var timing = regexp.MustCompile(`(\d{1,2}:)?\d{2}:\d{2}[.,]\d{3}`)
var tags = regexp.MustCompile(`<[^>]*>`)

func ParseSubtitles(b []byte, ext string) ([]domain.Segment, error) {
	if len(b) > 16<<20 {
		return nil, errors.New("subtitles too large")
	}
	out := []domain.Segment{}
	add := func(start, end int64, text string) {
		text = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(text, "")))
		if text != "" && start >= 0 && end > start {
			out = append(out, domain.Segment{StartMS: start, EndMS: end, Text: text, Origin: "platform_subtitles"})
		}
	}
	if ext == ".json" || ext == ".json3" {
		var v struct {
			Body []struct {
				From, To float64
				Content  string
			}
			Events []struct {
				Start    int64 `json:"tStartMs"`
				Duration int64 `json:"dDurationMs"`
				Segs     []struct {
					Text string `json:"utf8"`
				}
			}
		}
		if json.Unmarshal(b, &v) != nil {
			return nil, errors.New("invalid subtitle JSON")
		}
		for _, x := range v.Body {
			add(int64(math.Round(x.From*1000)), int64(math.Round(x.To*1000)), x.Content)
		}
		for _, x := range v.Events {
			var text strings.Builder
			for _, z := range x.Segs {
				text.WriteString(z.Text)
			}
			add(x.Start, x.Start+x.Duration, text.String())
		}
	} else {
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			if !strings.Contains(lines[i], "-->") {
				continue
			}
			times := timing.FindAllString(lines[i], -1)
			if len(times) != 2 {
				continue
			}
			text := []string{}
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				i++
				text = append(text, lines[i])
			}
			add(parseTime(times[0]), parseTime(times[1]), strings.Join(text, "\n"))
		}
	}
	slices.SortStableFunc(out, func(a, b domain.Segment) int {
		if a.StartMS < b.StartMS {
			return -1
		}
		if a.StartMS > b.StartMS {
			return 1
		}
		return 0
	})
	clean := []domain.Segment{}
	for _, x := range out {
		if len(clean) > 0 {
			prev := &clean[len(clean)-1]
			if x.Text == prev.Text && x.StartMS <= prev.EndMS {
				prev.EndMS = max(prev.EndMS, x.EndMS)
				continue
			}
		}
		x.Ordinal = len(clean)
		clean = append(clean, x)
	}
	return clean, nil
}
func parseTime(v string) int64 {
	parts := strings.Split(strings.ReplaceAll(v, ",", "."), ":")
	var seconds float64
	for _, x := range parts {
		n, e := strconv.ParseFloat(x, 64)
		if e != nil {
			return -1
		}
		seconds = seconds*60 + n
	}
	return int64(math.Round(seconds * 1000))
}
func TranscriptCoverage(segs []domain.Segment, duration int64) (bool, string) {
	if len(segs) == 0 || duration <= 0 {
		return false, "unknown"
	}
	covered := subtitleCoveredMS(segs, duration)
	ratio := float64(covered) / float64(duration)
	if ratio >= 0.85 {
		return true, "complete"
	}
	if ratio >= 0.5 {
		return true, "partial"
	}
	return false, "partial"
}

func subtitleCoveredMS(segs []domain.Segment, duration int64) int64 {
	var covered, end int64
	for _, x := range segs {
		if x.StartMS >= duration || x.EndMS > duration+1000 || strings.TrimSpace(x.Text) == "" {
			continue
		}
		start := max(end, x.StartMS)
		stop := min(duration, x.EndMS)
		if stop > start {
			covered += stop - start
		}
		end = max(end, stop)
	}
	return covered
}
