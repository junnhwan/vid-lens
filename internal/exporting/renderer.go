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

const RendererVersion = domain.ExportRendererVersion

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
	return markdownVersion(a, model, purpose, images, RendererVersion)
}

func markdownVersion(a domain.Article, model domain.GenerationModel, purpose string, images map[string]string, version int) ([]byte, error) {
	if version < 1 || version > RendererVersion {
		return nil, errors.New("unsupported export renderer version")
	}
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
	out.WriteString("解读详细程度：" + domain.DetailLabel(a.DetailLevel) + "\n\n")
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
		if version == 1 {
			for _, node := range a.MindMap.Nodes {
				out.WriteString("- " + escape(node.Label) + "\n")
			}
		} else if err := writeConceptMap(&out, *a.MindMap, a.Sections); err != nil {
			return nil, err
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
		untimedText := version >= 3 && c.Kind == "video_text" && c.TextOrigin != "platform_subtitles" && c.TextOrigin != "platform_subtitle"
		ms := int64(0)
		if c.TimeMS != nil {
			ms = *c.TimeMS
		}
		if c.Range != nil {
			ms = c.Range.StartMS
		}
		link := sourceURL(a.Source.CanonicalURL, ms)
		if untimedText {
			link = a.Source.CanonicalURL
		}
		u, e := safeURL(link)
		if e != nil {
			return nil, e
		}
		if untimedText {
			fmt.Fprintf(&out, "[^source-%d]: [视频原文](<%s>)", r.citations[id], u)
		} else {
			fmt.Fprintf(&out, "[^source-%d]: [%s · %s](<%s>)", r.citations[id], escape(c.Label), clock(ms), u)
		}
		if !untimedText && c.Range != nil {
			fmt.Fprintf(&out, "（至 %s）", clock(c.Range.EndMS))
		}
		if c.Quote != nil {
			out.WriteString(" — " + escape(*c.Quote))
		}
		out.WriteString("\n")
	}
	return []byte(out.String()), nil
}

func writeConceptMap(out *strings.Builder, tree domain.MindMap, sections []domain.Section) error {
	if len(tree.Nodes) == 0 || len(tree.Nodes) > 256 {
		return errors.New("invalid concept map size")
	}
	nodes := map[string]domain.MapNode{}
	children := map[string][]domain.MapNode{}
	for _, node := range tree.Nodes {
		if node.ID == "" {
			return errors.New("concept map node id is missing")
		}
		if _, exists := nodes[node.ID]; exists {
			return errors.New("duplicate concept map node")
		}
		nodes[node.ID] = node
		if node.ParentID != nil {
			children[*node.ParentID] = append(children[*node.ParentID], node)
		}
	}
	root, exists := nodes[tree.RootID]
	if !exists || root.ParentID != nil {
		return errors.New("invalid concept map root")
	}
	targets := map[string]int{}
	for index, section := range sections {
		targets[section.ID] = index + 1
	}
	seen := map[string]bool{}
	var visit func(domain.MapNode, int) error
	visit = func(node domain.MapNode, depth int) error {
		if seen[node.ID] {
			return errors.New("concept map cycle")
		}
		seen[node.ID] = true
		label := escape(strings.Join(strings.Fields(node.Label), " "))
		if node.SectionID != nil {
			index, ok := targets[*node.SectionID]
			if !ok {
				return errors.New("concept map chapter target is missing")
			}
			label = fmt.Sprintf("[%s](#section-%d)", label, index)
		}
		out.WriteString(strings.Repeat("  ", depth) + "- " + label + "\n")
		for _, child := range children[node.ID] {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return err
	}
	if len(seen) != len(nodes) {
		return errors.New("disconnected concept map")
	}
	return nil
}
