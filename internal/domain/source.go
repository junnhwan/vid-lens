package domain

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type SourceIdentity struct{ Platform, CanonicalID, PartKey, URL string }

var bv = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)
var yt = regexp.MustCompile(`^[0-9A-Za-z_-]{11}$`)

func ParseSource(raw string, local bool) (SourceIdentity, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	bad := func() (SourceIdentity, error) {
		return SourceIdentity{}, Err(422, "UNSUPPORTED_SOURCE", "请使用 B 站单个分 P 或 YouTube 单视频链接。")
	}
	if e != nil || u.User != nil {
		return bad()
	}
	if local && u.Scheme == "local" {
		name := u.Host
		if u.Path != "" || name == "" || path.Base(name) != name || strings.ContainsAny(name, "\\%") {
			return bad()
		}
		return SourceIdentity{"local", name, "1", "local://" + name}, nil
	}
	if u.Scheme != "https" {
		return bad()
	}
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" && u.Port() != "443" {
		return bad()
	}
	switch host {
	case "bilibili.com", "www.bilibili.com", "m.bilibili.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "video" || !bv.MatchString(parts[1]) {
			return bad()
		}
		p := u.Query().Get("p")
		if p == "" {
			p = "1"
		}
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 10000 {
			return bad()
		}
		p = strconv.Itoa(n)
		return SourceIdentity{"bilibili", parts[1], p, "https://www.bilibili.com/video/" + parts[1] + "?p=" + p}, nil
	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be":
		var id string
		if host == "youtu.be" {
			id = strings.Trim(u.Path, "/")
		} else {
			switch {
			case u.Path == "/watch":
				id = u.Query().Get("v")
			case strings.HasPrefix(u.Path, "/shorts/"):
				id = strings.TrimPrefix(u.Path, "/shorts/")
			}
		}
		if !yt.MatchString(id) {
			return bad()
		}
		return SourceIdentity{"youtube", id, "1", "https://www.youtube.com/watch?v=" + id}, nil
	}
	return bad()
}
func NormalizeQuote(s string) string { return strings.Join(strings.Fields(s), " ") }
func BlockText(b Block) string {
	in := func(a []Inline) string {
		var t strings.Builder
		for _, x := range a {
			if x.Type == "text" || x.Type == "link" {
				t.WriteString(x.Text)
			}
		}
		return t.String()
	}
	switch b.Type {
	case "paragraph", "quote":
		return in(b.Content)
	case "list":
		v := []string{}
		for _, x := range b.Items {
			v = append(v, in(x.Content))
		}
		return strings.Join(v, "\n")
	case "code":
		return b.Text
	case "table":
		v := append([]string{}, b.Columns...)
		for _, x := range b.Rows {
			v = append(v, x.Cells...)
		}
		return strings.Join(v, "\n")
	case "figure":
		return b.Caption
	}
	return ""
}
