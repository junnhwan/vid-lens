package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"vidlens/internal/config"
	"vidlens/internal/domain"
)

type bilibiliRoundTrip func(*http.Request) (*http.Response, error)

func (f bilibiliRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func bilibiliTestClient(t *testing.T, reply func(*http.Request) string) *http.Client {
	t.Helper()
	return &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("anonymous source sent credentials")
		}
		if r.Header.Get("Referer") != "https://www.bilibili.com/" {
			t.Fatal("missing media referer")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(reply(r))), Header: http.Header{}}, nil
	})}
}

func TestBilibiliPublicAPISelectsActualHighestTrackAndRequestedPart(t *testing.T) {
	client := bilibiliTestClient(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/x/web-interface/view":
			return `{"code":0,"data":{"title":"Fixture","pages":[{"page":1,"cid":101,"duration":20},{"page":2,"cid":102,"duration":10,"part":"Second"}]}}`
		case "/x/player/playurl":
			q := r.URL.Query()
			if q.Get("cid") != "102" || q.Get("qn") != "127" || q.Get("fnval") != "4048" || q.Get("try_look") != "1" {
				t.Fatalf("wrong part or quality request: %s", r.URL.RawQuery)
			}
			return `{"code":0,"data":{"timelength":10000,"accept_quality":[127,120,80],"dash":{"video":[{"id":16,"width":640,"height":360,"baseUrl":"https://cdn.example.test/360","bandwidth":9000000},{"id":120,"width":3840,"height":2160,"baseUrl":"https://cdn.example.test/4k","codecid":7,"bandwidth":5000000},{"id":127,"width":7680,"height":4320}],"audio":[{"id":1,"baseUrl":"https://cdn.example.test/audio","bandwidth":128000}]}}}`
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return ""
		}
	})
	s := &Service{Config: config.Config{CookieFile: "/account/never-readable.txt"}, bilibiliHTTP: client}
	identity, _ := domain.ParseSource("https://www.bilibili.com/video/BV1bK411W797?p=2", false)
	got, err := s.bilibiliReader().source(context.Background(), identity)
	if err != nil || got.CID != 102 || got.Video.Height != 2160 || got.Meta.Title != "Fixture — Second" {
		t.Fatalf("wrong source selection: %+v %v", got, err)
	}
}

func TestBilibiliRestrictedOrPreviewSourceCannotBecomeCompleteVideo(t *testing.T) {
	for _, mode := range []string{"login", "preview", "short_preview", "missing_part", "paid", "no_actual_tracks"} {
		t.Run(mode, func(t *testing.T) {
			client := bilibiliTestClient(t, func(r *http.Request) string {
				if r.URL.Path == "/x/web-interface/view" {
					if mode == "login" {
						return `{"code":-101,"message":"credential-do-not-echo"}`
					}
					if mode == "paid" {
						return `{"code":0,"data":{"rights":{"arc_pay":1}}}`
					}
					return `{"code":0,"data":{"title":"Fixture","pages":[{"page":1,"cid":1,"duration":90}]}}`
				}
				track := `"dash":{"video":[{"baseUrl":"https://cdn.example.test/video?sig=do-not-echo","width":640,"height":360}]}`
				switch mode {
				case "preview":
					return `{"code":0,"data":{"is_preview":true,` + track + `}}`
				case "short_preview":
					return `{"code":0,"data":{"timelength":30000,` + track + `}}`
				case "no_actual_tracks":
					return `{"code":0,"data":{"accept_quality":[127,120,80]}}`
				default:
					t.Fatalf("unexpected request for rejected source: %s", r.URL.Path)
					return ""
				}
			})
			part := "1"
			if mode == "missing_part" {
				part = "2"
			}
			_, err := (&bilibiliClient{http: client}).source(context.Background(), domain.SourceIdentity{Platform: "bilibili", CanonicalID: "BV1bK411W797", PartKey: part})
			var p *domain.Problem
			if !errors.As(err, &p) || strings.Contains(err.Error(), "do-not-echo") {
				t.Fatalf("source was accepted or leaked diagnostics: %v", err)
			}
		})
	}
}

func TestBilibiliAnonymousSubtitlesAndBoundedMedia(t *testing.T) {
	client := bilibiliTestClient(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/x/player/v2":
			return `{"code":0,"data":{"subtitle":{"subtitles":[{"lan":"zh-CN","subtitle_url":"//cdn.example.test/subtitle"}]}}}`
		case "/subtitle":
			return `{"body":[{"from":0,"to":5,"content":"Public transcript"}]}`
		case "/video":
			return "media bytes beyond quota"
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return ""
		}
	})
	b := &bilibiliClient{http: client}
	segments, lang, state := b.subtitles(context.Background(), bilibiliSource{BVID: "BV1bK411W797", CID: 1})
	if len(segments) != 1 || lang == nil || *lang != "zh-CN" || state != "available" {
		t.Fatal("public subtitles were not used")
	}
	err := b.download(context.Background(), bilibiliTrack{BaseURL: "https://cdn.example.test/video"}, filepath.Join(t.TempDir(), "video"), 4)
	var p *domain.Problem
	if !errors.As(err, &p) || p.Code != "INPUT_LIMIT_EXCEEDED" {
		t.Fatalf("byte quota bypassed: %v", err)
	}
}

func TestBilibiliNetworkFailuresDoNotLeakSignedURLs(t *testing.T) {
	client := &http.Client{Transport: bilibiliRoundTrip(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("https://cdn.example.test/?sig=credential-do-not-echo")
	})}
	_, err := (&bilibiliClient{http: client}).get(context.Background(), "https://cdn.example.test/?sig=credential-do-not-echo")
	if err == nil || strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), "https://") {
		t.Fatal("network failure leaked sensitive diagnostics")
	}
}
