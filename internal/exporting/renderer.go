package exporting

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"vidlens/internal/domain"
)

const RendererVersion = 1

func escape(s string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#", "!", "\\!", "|", "\\|", "~", "\\~").Replace(s)
}
func safeURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("unsupported link URL")
	}
	return strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20").Replace(u.String()), nil
}
func codeSpan(s string) string {
	n := 1
	for strings.Contains(s, strings.Repeat("`", n)) {
		n++
	}
	f := strings.Repeat("`", n)
	return f + " " + s + " " + f
}
func sourceURL(raw string, ms int64) string {
	u, e := url.Parse(raw)
	if e != nil {
		return raw
	}
	q := u.Query()
	q.Set("t", fmt.Sprint(ms/1000))
	u.RawQuery = q.Encode()
	return u.String()
}

type renderer struct {
	a         domain.Article
	images    map[string]string
	citations map[string]int
}

func (r renderer) refs(ids []string) (string, error) {
	out := ""
	for _, id := range ids {
		n, ok := r.citations[id]
		if !ok {
			return "", errors.New("missing citation")
		}
		out += fmt.Sprintf("[^source-%d]", n)
	}
	return out, nil
}
func (r renderer) inlines(items []domain.Inline) (string, error) {
	var out strings.Builder
	for _, in := range items {
		switch in.Type {
		case "text":
			text := escape(in.Text)
			for _, mark := range in.Marks {
				switch mark {
				case "code":
					text = codeSpan(in.Text)
				case "emphasis":
					text = "*" + text + "*"
				case "bold":
					text = "**" + text + "**"
				default:
					return "", errors.New("unsupported inline mark")
				}
			}
			out.WriteString(text)
		case "citation":
			v, e := r.refs([]string{in.CitationID})
			if e != nil {
				return "", e
			}
			out.WriteString(v)
		case "link":
			u, e := safeURL(in.URL)
			if e != nil {
				return "", e
			}
			fmt.Fprintf(&out, "[%s](<%s>)", escape(in.Text), u)
		default:
			return "", errors.New("unsupported inline type")
		}
	}
	return out.String(), nil
}
func (r renderer) block(b domain.Block) (string, error) {
	var out strings.Builder
	if b.Origin == "background_explanation" {
		out.WriteString("*补充解释*\n\n")
	}
	ref, e := r.refs(b.CitationIDs)
	if e != nil {
		return "", e
	}
	switch b.Type {
	case "paragraph", "quote":
		text, e := r.inlines(b.Content)
		if e != nil {
			return "", e
		}
		if b.Type == "quote" {
			text = "> " + strings.ReplaceAll(text, "\n", "\n> ")
		}
		out.WriteString(text + ref + "\n\n")
	case "list":
		for i, item := range b.Items {
			text, e := r.inlines(item.Content)
			if e != nil {
				return "", e
			}
			prefix := "- "
			if b.Ordered != nil && *b.Ordered {
				prefix = fmt.Sprintf("%d. ", i+1)
			}
			out.WriteString(prefix + strings.ReplaceAll(text, "\n", "\n    ") + "\n")
		}
		out.WriteString(ref + "\n\n")
	case "code":
		n := 3
		for strings.Contains(b.Text, strings.Repeat("`", n)) {
			n++
		}
		f := strings.Repeat("`", n)
		lang := ""
		if b.Language != nil && regexp.MustCompile(`^[A-Za-z0-9_+-]{0,40}$`).MatchString(*b.Language) {
			lang = *b.Language
		}
		out.WriteString(f + lang + "\n" + b.Text + "\n" + f + "\n" + ref + "\n\n")
	case "table":
		out.WriteString("| " + strings.Join(escapedCells(b.Columns), " | ") + " |\n| ")
		for range b.Columns {
			out.WriteString("--- | ")
		}
		out.WriteString("\n")
		for _, row := range b.Rows {
			rr, e := r.refs(row.CitationIDs)
			if e != nil {
				return "", e
			}
			out.WriteString("| " + strings.Join(escapedCells(row.Cells), " | ") + " | " + rr + "\n")
		}
		out.WriteString(ref + "\n\n")
	case "figure":
		if r.images != nil {
			for _, id := range b.FrameIDs {
				path, ok := r.images[id]
				if !ok {
					return "", errors.New("required image missing")
				}
				out.WriteString("![" + escape(b.Caption) + "](" + path + ")\n\n")
			}
		}
		out.WriteString("*图：" + escape(b.Caption) + "*" + ref + "\n\n")
		if r.images == nil {
			for _, id := range b.FrameIDs {
				for _, c := range r.a.Citations {
					if c.FrameID == id && c.TimeMS != nil {
						u, e := safeURL(sourceURL(r.a.Source.CanonicalURL, *c.TimeMS))
						if e != nil {
							return "", e
						}
						fmt.Fprintf(&out, "[回看原片 %s](<%s>)\n\n", clock(*c.TimeMS), u)
						break
					}
				}
			}
		}
	default:
		return "", errors.New("unsupported article block: " + b.Type)
	}
	return out.String(), nil
}
func escapedCells(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ReplaceAll(escape(s), "\n", "<br>")
	}
	return out
}
func clock(ms int64) string { return fmt.Sprintf("%02d:%02d", ms/60000, ms/1000%60) }
func Markdown(a domain.Article, model domain.GenerationModel, purpose string, images map[string]string) ([]byte, error) {
	r := renderer{a: a, images: images, citations: map[string]int{}}
	keys := make([]string, 0, len(a.Citations))
	for id := range a.Citations {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for i, id := range keys {
		r.citations[id] = i + 1
	}
	var out strings.Builder
	out.WriteString("# " + escape(a.Title) + "\n\n")
	url, e := safeURL(a.Source.CanonicalURL)
	if e != nil {
		return nil, e
	}
	fmt.Fprintf(&out, "原视频：[打开原片](<%s>)\n\n", url)
	if model.DisplayName != "" {
		fmt.Fprintf(&out, "生成模型：%s\n\n", escape(model.DisplayName))
	}
	if purpose != "" {
		fmt.Fprintf(&out, "本次关注点：%s\n\n", escape(purpose))
	}
	if images == nil {
		out.WriteString("> 此 Markdown 保存正文、图注与来源；完整配图请下载图文 ZIP。\n\n")
	}
	out.WriteString("## 目录\n\n")
	for i, sec := range a.Sections {
		fmt.Fprintf(&out, "- [%s](#section-%d)\n", escape(sec.Title), i+1)
	}
	out.WriteString("\n")
	for i, sec := range a.Sections {
		fmt.Fprintf(&out, "<a id=\"section-%d\"></a>\n\n## %s\n\n", i+1, escape(sec.Title))
		for _, b := range sec.Blocks {
			text, e := r.block(b)
			if e != nil {
				return nil, e
			}
			out.WriteString(text)
		}
	}
	if a.MindMap != nil {
		out.WriteString("## 文章导图\n\n")
		for _, node := range a.MindMap.Nodes {
			out.WriteString("- " + escape(node.Label) + "\n")
		}
		out.WriteString("\n")
	}
	if len(a.Limitations) > 0 {
		out.WriteString("## 还没确认的部分\n\n")
		for _, l := range a.Limitations {
			ref, e := r.refs(l.CitationIDs)
			if e != nil {
				return nil, e
			}
			out.WriteString("- " + escape(l.Message) + ref + "\n")
		}
		out.WriteString("\n")
	}
	out.WriteString("## 来源\n\n")
	for _, id := range keys {
		c := a.Citations[id]
		ms := int64(0)
		if c.TimeMS != nil {
			ms = *c.TimeMS
		}
		if c.Range != nil {
			ms = c.Range.StartMS
		}
		u, e := safeURL(sourceURL(a.Source.CanonicalURL, ms))
		if e != nil {
			return nil, e
		}
		fmt.Fprintf(&out, "[^source-%d]: [%s · %s](<%s>)", r.citations[id], escape(c.Label), clock(ms), u)
		if c.Range != nil {
			fmt.Fprintf(&out, "（至 %s）", clock(c.Range.EndMS))
		}
		if c.Quote != nil {
			out.WriteString(" — " + escape(*c.Quote))
		}
		out.WriteString("\n")
	}
	return []byte(out.String()), nil
}
