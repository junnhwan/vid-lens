package media

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/outbound"
)

// Bilibili's public web API is read without a cookie jar, browser session or
// owner credentials. The public reader deliberately uses a direct connection;
// the configured extractor/model proxy is not an account fallback.
type bilibiliClient struct{ http *http.Client }

type bilibiliTrack struct {
	ID        int      `json:"id"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Bandwidth int64    `json:"bandwidth"`
	CodecID   int      `json:"codecid"`
	FrameRate string   `json:"frameRate"`
	BaseURL   string   `json:"baseUrl"`
	BackupURL []string `json:"backupUrl"`
}

type bilibiliSource struct {
	Meta         metadata
	BVID         string
	CID          int64
	Video, Audio bilibiliTrack
	Heights      []int
}

func (s *Service) bilibiliReader() *bilibiliClient {
	if s.bilibiliHTTP != nil {
		return &bilibiliClient{http: s.bilibiliHTTP}
	}
	return &bilibiliClient{http: outbound.Client(nil, 0, 3)}
}

func bilibiliFailure(ctx context.Context, restricted bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if restricted {
		return domain.Err(422, "SOURCE_ACCESS_RESTRICTED", "B 站当前限制了匿名访问，或这个视频需要登录、付费。请稍后重试或换一个公开视频；服务不会使用站长账号。")
	}
	return domain.Err(422, "SOURCE_UNAVAILABLE", "暂时无法匿名获取这个 B 站视频，请稍后重试或换一个公开视频。")
}

func (b *bilibiliClient) get(ctx context.Context, raw string) (*http.Response, error) {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		raw = "https://" + strings.TrimPrefix(raw, "http://")
	}
	if _, err := outbound.Validate(raw, nil, true); err != nil {
		return nil, bilibiliFailure(ctx, false)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, bilibiliFailure(ctx, false)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	resp, err := b.http.Do(req)
	if err != nil {
		// URL errors can contain signed CDN links. Never return them publicly.
		return nil, bilibiliFailure(ctx, false)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, bilibiliFailure(ctx, resp.StatusCode == 403 || resp.StatusCode == 412 || resp.StatusCode == 429)
	}
	return resp, nil
}

func (b *bilibiliClient) read(ctx context.Context, raw string, limit int64) ([]byte, error) {
	resp, err := b.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, bilibiliFailure(ctx, false)
	}
	return data, nil
}

func (b *bilibiliClient) api(ctx context.Context, path string, query url.Values, dst any) error {
	data, err := b.read(ctx, "https://api.bilibili.com"+path+"?"+query.Encode(), 4<<20)
	if err != nil {
		return err
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code == nil {
		return bilibiliFailure(ctx, false)
	}
	if *envelope.Code != 0 {
		return bilibiliFailure(ctx, *envelope.Code != -404)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, dst) != nil {
		return bilibiliFailure(ctx, false)
	}
	return nil
}

func (b *bilibiliClient) source(ctx context.Context, identity domain.SourceIdentity) (bilibiliSource, error) {
	var out bilibiliSource
	var view struct {
		Title             string         `json:"title"`
		IsUpowerExclusive bool           `json:"is_upower_exclusive"`
		RawRights         map[string]int `json:"rights"`
		Pages             []struct {
			CID      int64  `json:"cid"`
			Page     int    `json:"page"`
			Part     string `json:"part"`
			Duration int    `json:"duration"`
		} `json:"pages"`
	}
	if err := b.api(ctx, "/x/web-interface/view", url.Values{"bvid": {identity.CanonicalID}}, &view); err != nil {
		return out, err
	}
	if view.IsUpowerExclusive || view.RawRights["arc_pay"] != 0 || view.RawRights["pay"] != 0 || view.RawRights["ugc_pay"] != 0 {
		return out, bilibiliFailure(ctx, true)
	}
	if view.RawRights["is_stein_gate"] != 0 {
		return out, domain.Err(422, "UNSUPPORTED_SOURCE", "暂不处理 B 站互动视频，请使用普通单个视频或分 P。")
	}
	part, _ := strconv.Atoi(identity.PartKey)
	for _, page := range view.Pages {
		if page.Page == part && page.CID > 0 {
			out.BVID, out.CID = identity.CanonicalID, page.CID
			out.Meta = metadata{ID: identity.CanonicalID, Title: view.Title, Duration: float64(page.Duration)}
			if len(view.Pages) > 1 && page.Part != "" {
				out.Meta.Title += " — " + page.Part
			}
			break
		}
	}
	if out.CID == 0 {
		return out, domain.Err(422, "SOURCE_UNAVAILABLE", "这个 B 站分 P 不存在或无法匿名读取。")
	}
	var play struct {
		TimeLength int64           `json:"timelength"`
		IsPreview  json.RawMessage `json:"is_preview"`
		Dash       struct {
			Video []bilibiliTrack `json:"video"`
			Audio []bilibiliTrack `json:"audio"`
		} `json:"dash"`
	}
	// Ask for the highest public quality; select from actual returned tracks,
	// never from the advertised accept_quality list. There is no 1080p cap.
	query := url.Values{"bvid": {out.BVID}, "cid": {strconv.FormatInt(out.CID, 10)}, "qn": {"127"}, "fnval": {"4048"}, "fourk": {"1"}, "try_look": {"1"}}
	if err := b.api(ctx, "/x/player/playurl", query, &play); err != nil {
		return out, err
	}
	if string(play.IsPreview) == "true" || string(play.IsPreview) == "1" || (play.TimeLength > 0 && float64(play.TimeLength)+2000 < out.Meta.Duration*1000) {
		return out, bilibiliFailure(ctx, true)
	}
	var found bool
	out.Video, found = bestBilibiliTrack(play.Dash.Video, true)
	if !found {
		return out, bilibiliFailure(ctx, true)
	}
	out.Audio, _ = bestBilibiliTrack(play.Dash.Audio, false)
	for _, track := range play.Dash.Video {
		if track.BaseURL != "" && track.Height > 0 && !slices.Contains(out.Heights, track.Height) {
			out.Heights = append(out.Heights, track.Height)
		}
	}
	slices.Sort(out.Heights)
	return out, nil
}

func bestBilibiliTrack(tracks []bilibiliTrack, video bool) (bilibiliTrack, bool) {
	var best bilibiliTrack
	found := false
	for _, track := range tracks {
		if track.BaseURL == "" || (video && (track.Height <= 0 || track.Width <= 0)) {
			continue
		}
		better := !found || track.Bandwidth > best.Bandwidth
		if video {
			better = !found || track.Height > best.Height || (track.Height == best.Height && frameRate(track.FrameRate) > frameRate(best.FrameRate)) ||
				(track.Height == best.Height && frameRate(track.FrameRate) == frameRate(best.FrameRate) && ((track.CodecID == 7 && best.CodecID != 7) || (track.CodecID == best.CodecID && track.Bandwidth > best.Bandwidth)))
		}
		if better {
			best, found = track, true
		}
	}
	return best, found
}

func frameRate(raw string) float64 {
	a, c, fraction := strings.Cut(raw, "/")
	n, _ := strconv.ParseFloat(a, 64)
	if fraction {
		d, _ := strconv.ParseFloat(c, 64)
		if d > 0 {
			return n / d
		}
	}
	return n
}

func (b *bilibiliClient) subtitles(ctx context.Context, source bilibiliSource) ([]domain.Segment, *string, string) {
	segs, lang, status, _ := b.subtitleDecision(ctx, source)
	return segs, lang, status
}

func (b *bilibiliClient) subtitleDecision(ctx context.Context, source bilibiliSource) ([]domain.Segment, *string, string, subtitleDecision) {
	d := subtitleDecision{Revision: "subtitle-adoption-v2", Candidates: []subtitleChoice{}, Attempts: []subtitleAttempt{}, FallbackReason: "metadata_failed"}
	var info struct {
		NeedLogin bool `json:"need_login_subtitle"`
		Subtitle  struct {
			Tracks []struct {
				Language string `json:"lan"`
				URL      string `json:"subtitle_url"`
			} `json:"subtitles"`
		} `json:"subtitle"`
	}
	query := url.Values{"bvid": {source.BVID}, "cid": {strconv.FormatInt(source.CID, 10)}}
	if b.api(ctx, "/x/player/v2", query, &info) != nil {
		return nil, nil, "unavailable", d
	}
	if info.NeedLogin {
		d.FallbackReason = "login_required"
		return nil, nil, "login_required", d
	}
	m := metadata{Subtitles: map[string][]subformat{}}
	for _, track := range info.Subtitle.Tracks {
		if track.URL != "" {
			m.Subtitles[track.Language] = []subformat{{Ext: "json"}}
		}
	}
	d.Candidates = subtitleChoices(m)
	for i := range d.Candidates {
		d.Candidates[i].Kind = "platform_unspecified"
	}
	d.FallbackReason = "no_supported_track"
	for _, choice := range d.Candidates[:min(3, len(d.Candidates))] {
		choice.Kind = "platform_unspecified"
		for _, track := range info.Subtitle.Tracks {
			if track.Language != choice.Language {
				continue
			}
			attempt := subtitleAttempt{Choice: choice, Download: "failed", Parse: "not_attempted", Coverage: "unknown"}
			data, err := b.read(ctx, track.URL, 16<<20)
			var segments []domain.Segment
			if err == nil {
				attempt.Download = "received"
				attempt.Parse = "invalid"
				segments, err = ParseSubtitles(data, ".json")
				if err == nil && len(segments) > 0 {
					attempt.Parse = "parsed"
				}
			}
			attempt.Segments = len(segments)
			attempt.Usable, attempt.Coverage = TranscriptCoverage(segments, int64(source.Meta.Duration*1000))
			d.Attempts = append(d.Attempts, attempt)
			if err == nil && len(segments) > 0 && (attempt.Usable || source.Meta.Duration <= 0) {
				d.Selected = &choice
				d.FallbackReason = ""
				return segments, &choice.Language, "available", d
			}
			d.FallbackReason = "parse_failed"
			if attempt.Parse == "parsed" {
				d.FallbackReason = "coverage_below_threshold"
			}
			if attempt.Download != "received" {
				d.FallbackReason = "download_failed"
			}
		}
	}
	return nil, nil, "unavailable", d
}

func (b *bilibiliClient) download(ctx context.Context, track bilibiliTrack, dst string, limit int64) error {
	if limit <= 0 {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "音视频累计大小超过上限。")
	}
	if track.BaseURL == "" {
		return bilibiliFailure(ctx, false)
	}
	// Each track is one bounded stream. Existing workspace and byte admission
	// remain authoritative, including when the highest public track is 4K.
	resp, err := b.get(ctx, track.BaseURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > limit {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "音视频累计大小超过上限。")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := &progressReader{r: resp.Body}
	reader.last.Store(time.Now().UnixNano())
	var stalled atomic.Bool
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				resp.Body.Close()
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, reader.last.Load())) > 60*time.Second {
					stalled.Store(true)
					cancel()
					resp.Body.Close()
					return
				}
			}
		}
	}()
	_, _, err = copyBounded(ctx, dst, reader, limit)
	if err != nil {
		if stalled.Load() {
			return &domain.WorkError{Code: "SOURCE_UNAVAILABLE", Message: "B 站下载长时间没有字节进展。", Transient: true}
		}
		if _, ok := err.(*domain.Problem); ok {
			return err
		}
		return bilibiliFailure(ctx, false)
	}
	return nil
}
