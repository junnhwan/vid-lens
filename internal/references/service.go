package references

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/net/html"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/outbound"
	"vidlens/internal/resource"
	"vidlens/internal/store"
)

const MaxPageBytes = 2 << 20
const MaxBodyChars = 12000

type Service struct {
	Store     *store.Store
	Resources *resource.Manager
	Config    config.Config
	Client    *http.Client
	SearchURL string
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return outbound.Client(nil, 20*time.Second, 3)
}
func KnownURLs(text string) []string {
	matches := regexp.MustCompile(`https?://[^\s<>"\x00-\x1f]+`).FindAllString(text, 20)
	out := []string{}
	for _, v := range matches {
		v = strings.TrimRight(v, ".,;!?，。；！？）)]}")
		if _, e := outbound.Validate(v, nil, false); e == nil {
			out = append(out, v)
		}
	}
	return out
}
func Text(body []byte) (string, string, bool, error) {
	n, e := html.Parse(bytes.NewReader(body))
	if e != nil {
		return "", "", false, e
	}
	title := ""
	var text strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inTitle bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "nav", "footer", "header", "form", "svg", "iframe", "template":
				return
			case "title":
				inTitle = true
			case "p", "div", "section", "article", "main", "li", "h1", "h2", "h3", "br":
				text.WriteByte('\n')
			}
		}
		if n.Type == html.TextNode {
			if inTitle {
				title += n.Data
			} else {
				text.WriteString(n.Data + " ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inTitle)
		}
	}
	walk(n, false)
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	lines := []string{}
	for _, l := range strings.Split(text.String(), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	joined := strings.Join(lines, "\n")
	runes := []rune(joined)
	truncated := len(runes) > MaxBodyChars
	if truncated {
		joined = string(runes[:MaxBodyChars])
	}
	if strings.TrimSpace(joined) == "" {
		return "", "", false, errors.New("page has no readable text")
	}
	return title, joined, truncated, nil
}
func Fetch(ctx context.Context, client *http.Client, raw string) (string, string, string, bool, error) {
	if _, e := outbound.Validate(raw, nil, false); e != nil {
		return "", "", "", false, e
	}
	r, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return "", "", "", false, e
	}
	r.Header.Set("Accept", "text/html,application/xhtml+xml")
	r.Header.Set("Accept-Encoding", "identity")
	r.Header.Set("User-Agent", "VidLens-PublicReader/1.0")
	resp, e := client.Do(r)
	if e != nil {
		return "", "", "", false, errors.New("public page request failed")
	}
	defer resp.Body.Close()
	if _, e = outbound.Validate(resp.Request.URL.String(), nil, false); e != nil {
		return "", "", "", false, e
	}
	if resp.StatusCode != 200 {
		return "", "", "", false, errors.New("page did not return readable content")
	}
	ct, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || (ct != "text/html" && ct != "application/xhtml+xml") {
		return "", "", "", false, errors.New("only public HTML is supported")
	}
	rawBody, e := io.ReadAll(io.LimitReader(resp.Body, MaxPageBytes+1))
	if e != nil || len(rawBody) > MaxPageBytes {
		return "", "", "", false, errors.New("compressed response exceeds limit")
	}
	var reader io.Reader = bytes.NewReader(rawBody)
	if encoding := resp.Header.Get("Content-Encoding"); encoding == "gzip" {
		g, e := gzip.NewReader(reader)
		if e != nil {
			return "", "", "", false, e
		}
		defer g.Close()
		reader = g
	} else if encoding != "" && encoding != "identity" {
		return "", "", "", false, errors.New("unsupported content encoding")
	}
	body, e := io.ReadAll(io.LimitReader(reader, MaxPageBytes+1))
	if e != nil || len(body) > MaxPageBytes {
		return "", "", "", false, errors.New("HTML response exceeds limit")
	}
	title, text, truncated, e := Text(body)
	return title, text, resp.Request.URL.String(), truncated, e
}
func (s *Service) Read(ctx context.Context, c domain.Claim, id, key string) (map[string]any, error) {
	if !s.Config.ReferenceReadEnabled {
		return nil, domain.Err(403, "EXTERNAL_SOURCES_DISABLED", "网络补充当前未启用。")
	}
	r, op, e := s.Store.StartReferenceRead(ctx, c, id, key)
	if e != nil {
		return nil, e
	}
	if op.Status == "completed" {
		var v map[string]any
		e = json.Unmarshal(op.Result, &v)
		return v, e
	}
	client := s.client()
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	title, text, url, truncated, e := Fetch(ctx, client, r.URL)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = s.Store.FailedReference(ctx, c, id)
		v := map[string]any{"reference_id": id, "status": "failed", "limitation": "网页无法读取，不能依据搜索摘要当作已核实事实。"}
		if e = s.Store.FinishOperation(ctx, c, key, v, "", nil); e != nil {
			return nil, e
		}
		return v, nil
	}
	h := sha256.Sum256([]byte(text))
	sha := hex.EncodeToString(h[:])
	if e = s.Store.SaveReference(ctx, c, id, key, title, url, text, sha, truncated); e != nil {
		return nil, e
	}
	r, e = s.Store.ReferenceForRun(ctx, c.Run.ID, id)
	if e != nil {
		return nil, e
	}
	v := r.Evidence()
	if e = s.Store.FinishOperation(ctx, c, key, v, "", nil); e != nil {
		return nil, e
	}
	return v, nil
}
func (s *Service) Search(ctx context.Context, c domain.Claim, t domain.Turn, query, key string) (map[string]any, error) {
	if !s.Config.ReferenceReadEnabled || !s.Config.SearchEnabled || s.Config.TavilyAPIKey == "" {
		return nil, domain.Err(403, "SEARCH_DISABLED", "免费搜索尚未配置或已关闭，可读取已知公开网页。")
	}
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 500 {
		return nil, domain.Err(422, "INVALID_ARGUMENT", "搜索词为空或过长。")
	}
	op, e := s.Store.BeginOperation(ctx, c, key, "web.search", map[string]string{"query": query}, false)
	if e != nil {
		return nil, e
	}
	if op.Status == "completed" {
		var v map[string]any
		e = json.Unmarshal(op.Result, &v)
		return v, e
	}
	actx, release, e := s.Resources.Acquire(ctx, c, "web:search")
	if e != nil {
		return nil, e
	}
	defer release()
	if e = s.Store.ReserveSearch(actx, c, key, s.Config.SearchMonthlyLimit); e != nil {
		return nil, e
	}
	body := domain.JSON(map[string]any{"query": query, "search_depth": "basic", "auto_parameters": false, "include_answer": false, "include_raw_content": false, "include_images": false, "include_usage": true, "max_results": 5})
	endpoint := s.SearchURL
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	req, _ := http.NewRequestWithContext(actx, "POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Config.TavilyAPIKey)
	client := s.client()
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := client.Do(req)
	if e != nil {
		return nil, domain.Err(409, "PROVIDER_OUTCOME_UNKNOWN", "搜索结果未知，已保守计入额度，本轮不会重复发送。")
	}
	defer response.Body.Close()
	v := map[string]any{"candidates": []any{}, "note": "Search candidates are not evidence. Read the actual page using read_reference before citing it."}
	if response.StatusCode != 200 {
		if response.StatusCode == 402 || response.StatusCode == 429 || response.StatusCode == 432 || response.StatusCode == 433 {
			_ = s.Store.ExhaustSearchMonth(actx)
		}
		v["limitation"] = "免费搜索暂不可用，继续基于视频证据回答。"
		if e = s.Store.FinishOperation(actx, c, key, v, "", nil); e != nil {
			return nil, e
		}
		return v, nil
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, MaxPageBytes+1))
	if e != nil || len(b) > MaxPageBytes {
		return nil, domain.Err(409, "PROVIDER_OUTCOME_UNKNOWN", "搜索响应不完整，已保守计入额度。")
	}
	var data struct {
		Results []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"results"`
		Usage struct {
			Credits int `json:"credits"`
		} `json:"usage"`
	}
	if json.Unmarshal(b, &data) != nil {
		return nil, domain.Err(409, "PROVIDER_OUTCOME_UNKNOWN", "搜索响应无法核实，已保守计入额度。")
	}
	if data.Usage.Credits > 1 {
		_ = s.Store.ExhaustSearchMonth(actx)
		v["limitation"] = "服务报告异常额度消耗，后续搜索已停止。"
	} else {
		candidates := []map[string]any{}
		for i, item := range data.Results {
			if i >= 5 {
				break
			}
			if _, e = outbound.Validate(item.URL, nil, false); e != nil {
				continue
			}
			r, e := s.Store.RegisterReference(actx, c, t, item.URL, item.Title)
			if e != nil {
				return nil, e
			}
			candidates = append(candidates, map[string]any{"reference_id": r.ID, "url": r.URL, "title": r.Title, "status": "candidate_not_read"})
		}
		v["candidates"] = candidates
	}
	if e = s.Store.FinishOperation(actx, c, key, v, "", nil); e != nil {
		return nil, e
	}
	return v, nil
}
