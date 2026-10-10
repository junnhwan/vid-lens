package summarydoc

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Markdown is a text export of the same document version. Figures become
// captions and actual video times; no screenshot URL or object key is emitted.
func Markdown(doc Document) (string, error) {
	data, err := CanonicalJSON(doc)
	if err != nil {
		return "", err
	}
	// The projection uses exactly the ordering represented by the content digest.
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("# " + plain(doc.Title) + "\n\n")
	if doc.Overview != "" {
		out.WriteString(doc.Overview + "\n\n")
	}
	depth := map[string]int{}
	for _, b := range OrderedBlocks(doc) {
		d := 2
		if b.ParentID != nil {
			d = depth[*b.ParentID] + 1
		}
		depth[b.ID] = d
		if b.Title != "" {
			level := d
			if level > 6 {
				level = 6
			}
			out.WriteString(strings.Repeat("#", level) + " " + plain(b.Title) + "\n\n")
		}
		if b.BodyMarkdown != "" {
			out.WriteString(b.BodyMarkdown + "\n\n")
		}
		for _, f := range b.Figures {
			out.WriteString("图：" + plain(f.Caption) + "（视频 " + timeLabel(*f.CaptureMS) + "）\n")
			if f.Alt != f.Caption {
				out.WriteString("画面说明：" + plain(f.Alt) + "\n")
			}
			if f.Supports != "" {
				out.WriteString("说明：" + plain(f.Supports) + "\n")
			}
			out.WriteByte('\n')
		}
		if len(b.SourceRefs) > 0 {
			var times []string
			seen := map[string]bool{}
			for _, ref := range b.SourceRefs {
				label := "时间未知"
				if ref.StartMS != nil && ref.EndMS != nil {
					label = timeLabel(*ref.StartMS) + "–" + timeLabel(*ref.EndMS) + "（" + ref.TimingMethod + "）"
				}
				if !seen[label] {
					times = append(times, label)
					seen[label] = true
				}
			}
			out.WriteString("来源时间：" + strings.Join(times, "；") + "\n\n")
		}
	}
	return strings.TrimSpace(out.String()) + "\n", nil
}

func timeLabel(ms int64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d.%03d", s/3600, (s/60)%60, s%60, ms%1000)
	}
	return fmt.Sprintf("%02d:%02d.%03d", s/60, s%60, ms%1000)
}

func plain(text string) string {
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#", "\r", " ", "\n", " ")
	return r.Replace(text)
}
