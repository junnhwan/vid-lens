package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

const asrPipelineVersion = "60s-mp3-48k16k-v1"

func (s *Service) Transcribe(ctx context.Context, c domain.Claim, p config.Profile, input string, duration int64, work string, progress func(int64) error) ([]domain.Segment, error) {
	if p.ASRBaseURL == "" || p.ASRModel == "" {
		return nil, domain.Err(422, "PROVIDER_UNAVAILABLE", "没有可用字幕，请先配置 ASR 服务。")
	}
	key, e := s.Store.Credential(ctx, p.ASRKeyRef)
	if e != nil {
		return nil, e
	}
	allctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	segments := []domain.Segment{}
	for start := int64(0); start < duration; start += 60000 {
		end := min(duration, start+60000)
		opkey := fmt.Sprintf("asr:%d:%d", start, end)
		args := map[string]any{"start_ms": start, "end_ms": end, "model": p.ASRModel}
		if old, e := s.Store.Operation(allctx, c.Run.ID, opkey); e == nil && old.Status == "completed" {
			var saved []domain.Segment
			if json.Unmarshal(old.Result, &saved) != nil {
				return nil, fmt.Errorf("invalid ASR checkpoint")
			}
			segments = append(segments, saved...)
			if progress != nil {
				if e := progress(end); e != nil {
					return nil, e
				}
			}
			continue
		}
		file := filepath.Join(work, fmt.Sprintf("asr-%d.mp3", start))
		fctx, rel, e := s.Resources.Acquire(allctx, c, "ffmpeg")
		if e != nil {
			return nil, e
		}
		sub, cc := context.WithTimeout(fctx, 5*time.Minute)
		_, e = command(sub, "ffmpeg", []string{"-nostdin", "-v", "error", "-threads", "2", "-ss", fmt.Sprintf("%.3f", float64(start)/1000), "-i", input, "-t", fmt.Sprintf("%.3f", float64(end-start)/1000), "-vn", "-ac", "1", "-ar", "16000", "-filter_threads", "1", "-threads", "2", "-b:a", "48k", "-y", file}, work, p.WorkspaceBytes, 0)
		cc()
		rel()
		if e != nil {
			return nil, e
		}
		asset, e := s.own(allctx, c, file, "audio", "audio/mpeg", nil, time.Hour)
		if e != nil {
			return nil, e
		}
		if e = s.Store.AddAsset(allctx, c, asset); e != nil {
			return nil, e
		}
		file, _ = s.Path(asset.StorageKey)
		var result []domain.Segment
		for local := 0; local < 2; local++ {
			op, e := s.Store.BeginOperation(allctx, c, opkey, "asr", args, true)
			if e != nil {
				return nil, e
			}
			if op.Status == "completed" {
				_ = json.Unmarshal(op.Result, &result)
				break
			}
			actx, release, e := s.Resources.Acquire(allctx, c, "ai")
			if e != nil {
				return nil, e
			}
			sub, cc := context.WithTimeout(actx, 180*time.Second)
			raw, requestID, status, e := s.asrRequest(sub, p, key, file)
			cc()
			release()
			if e != nil {
				if local == 0 && op.Submissions < 2 {
					continue
				}
				return nil, &domain.WorkError{Code: "PROVIDER_UNAVAILABLE", Message: "ASR 响应未能确认。", Transient: op.Submissions < 2}
			}
			if status != 200 {
				return nil, providerFailure(status)
			}
			var decoded struct {
				Text     string `json:"text"`
				Segments []struct {
					Start, End float64
					Text       string
				}
			}
			if json.Unmarshal(raw, &decoded) != nil {
				return nil, domain.Err(422, "PROVIDER_UNAVAILABLE", "ASR 返回格式无效。")
			}
			if len(decoded.Segments) > 0 {
				for _, x := range decoded.Segments {
					a := start + int64(x.Start*1000)
					b := min(end, start+int64(x.End*1000))
					if a >= start && a < b && strings.TrimSpace(x.Text) != "" {
						result = append(result, domain.Segment{StartMS: a, EndMS: b, Text: x.Text, Origin: "asr"})
					}
				}
			} else if strings.TrimSpace(decoded.Text) != "" {
				result = []domain.Segment{{StartMS: start, EndMS: end, Text: strings.TrimSpace(decoded.Text), Origin: "asr"}}
			}
			if result == nil {
				result = []domain.Segment{}
			}
			if e = s.Store.FinishOperation(allctx, c, opkey, result, requestID, map[string]any{"timestamp_precision": "audio_window_unless_provider_segments"}); e != nil {
				return nil, e
			}
			break
		}
		segments = append(segments, result...)
		if progress != nil {
			if e := progress(end); e != nil {
				return nil, e
			}
		}
	}
	for i := range segments {
		segments[i].Ordinal = i
	}
	return segments, nil
}
func (s *Service) asrRequest(ctx context.Context, p config.Profile, key, file string) ([]byte, string, int, error) {
	endpoint := strings.TrimRight(p.ASRBaseURL, "/")
	if !strings.HasSuffix(endpoint, "/audio/transcriptions") {
		endpoint += "/audio/transcriptions"
	}

	f, e := os.Open(file)
	if e != nil {
		return nil, "", 0, e
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil {
		return nil, "", 0, e
	}
	if stat.Size() > 50<<20 {
		return nil, "", 0, domain.Err(422, "INPUT_LIMIT_EXCEEDED", "ASR 音频超过上传限制。")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if e = mw.WriteField("model", p.ASRModel); e != nil {
		return nil, "", 0, e
	}
	if _, e = mw.CreateFormFile("file", filepath.Base(file)); e != nil {
		return nil, "", 0, e
	}
	prefix := bytes.Clone(buf.Bytes())
	buf.Reset()
	if e = mw.Close(); e != nil {
		return nil, "", 0, e
	}
	suffix := bytes.Clone(buf.Bytes())
	reader := io.MultiReader(bytes.NewReader(prefix), &contextReader{ctx: ctx, r: f}, bytes.NewReader(suffix))
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, reader)
	if e != nil {
		return nil, "", 0, e
	}
	req.ContentLength = int64(len(prefix)+len(suffix)) + stat.Size()

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, ResponseHeaderTimeout: 90 * time.Second}
	if s.Config.ProxyURL != "" {
		u, e := url.Parse(s.Config.ProxyURL)
		if e != nil {
			return nil, "", 0, e
		}
		tr.Proxy = http.ProxyURL(u)
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 180 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		return nil, "", 0, e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if len(b) > 4<<20 {
		return nil, "", 0, fmt.Errorf("ASR response too large")
	}
	return b, res.Header.Get("x-siliconcloud-trace-id"), res.StatusCode, e
}
func providerFailure(status int) error {
	code := "PROVIDER_UNAVAILABLE"
	retry := status == 408 || status == 429 || status >= 500
	if status == 429 {
		code = "PROVIDER_RATE_LIMITED"
	}
	return &domain.WorkError{Code: code, Message: "远端服务未能完成当前操作。", Transient: retry}
}
