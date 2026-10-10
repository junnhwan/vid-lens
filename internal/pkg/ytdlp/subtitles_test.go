package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureBV = "BV1xx411c7mD"
const identityFixture = `{"code":0,"data":{"bvid":"BV1xx411c7mD","aid":42,"title":"Fixture","rights":{"is_stein_gate":0},"pages":[{"cid":111,"page":1,"duration":30,"part":"First"},{"cid":222,"page":2,"duration":40,"part":"Second"}]}}`
const subtitleFixture = `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40,"subtitles":{"danmaku":[{"ext":"xml","url":"https://comment.bilibili.com/222.xml"}],"en":[{"ext":"srt","data":"English","subtitle_kind":"manual","kind_basis":"provider explicit manual type"}],"zh-Hans":[{"ext":"srt","data":"1\n00:00:00,000 --> 00:00:01,000\n字幕\n","ai_status":0}]}}`

type fixtureGetter func(context.Context, string) ([]byte, string, error)

func (f fixtureGetter) Get(ctx context.Context, raw string) ([]byte, string, error) {
	return f(ctx, raw)
}

func fixtureIdentity(t *testing.T) BilibiliIdentity {
	t.Helper()
	id, err := parseIdentity([]byte(identityFixture), fixtureBV, 2)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestResolveIdentityBVAVShortLinkAndPart(t *testing.T) {
	requests := []string{}
	a, err := NewAdapter(Config{HTTPClient: fixtureGetter(func(_ context.Context, raw string) ([]byte, string, error) {
		requests = append(requests, raw)
		if strings.Contains(raw, "b23.tv") {
			return []byte("redirected"), "https://www.bilibili.com/video/" + fixtureBV + "?p=2&tracking=secret", nil
		}
		return []byte(identityFixture), raw, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://www.bilibili.com/video/" + fixtureBV + "?p=2&tracking=x", "https://bilibili.com/video/av42?p=2", "https://b23.tv/short"} {
		id, err := a.ResolveIdentity(context.Background(), raw)
		if err != nil || id.BVID != fixtureBV || id.AID != 42 || id.CID != 222 || id.PartIndex != 2 || id.CanonicalURL != "https://www.bilibili.com/video/"+fixtureBV+"?p=2" || id.DurationMS != 40000 {
			t.Fatalf("identity=%+v err=%v", id, err)
		}
	}
	for _, raw := range requests {
		if strings.Contains(raw, "/x/web-interface/view") && strings.Contains(raw, "tracking") {
			t.Fatalf("tracking entered metadata request: %s", raw)
		}
	}
	first, err := a.ResolveIdentity(context.Background(), "https://www.bilibili.com/video/"+fixtureBV)
	if err != nil || first.CID != 111 || first.PartIndex != 1 || strings.Contains(first.CanonicalURL, "?p=") {
		t.Fatalf("first part=%+v %v", first, err)
	}
}

func TestResolveRejectsUnsupportedMismatchedAndOutOfRange(t *testing.T) {
	a, _ := NewAdapter(Config{HTTPClient: fixtureGetter(func(_ context.Context, raw string) ([]byte, string, error) {
		if strings.Contains(raw, "b23.tv") {
			return nil, "https://evil.test/video/" + fixtureBV, nil
		}
		return []byte(identityFixture), raw, nil
	})})
	for _, raw := range []string{"https://www.bilibili.com/video/" + fixtureBV + "?p=3", "https://www.bilibili.com/video/" + fixtureBV + "?p=1&p=2", "https://www.bilibili.com/bangumi/play/ep1", "https://www.bilibili.com/list/1", "https://b23.tv/evil", "https://www.bilibili.com/video/av43", "https://www.bilibili.com/video/BV1xx411c7mE", "https://bilibili.com:8080/video/" + fixtureBV} {
		if _, err := a.ResolveIdentity(context.Background(), raw); err == nil {
			t.Fatalf("unsupported input accepted %s", raw)
		}
	}
	for _, raw := range []string{strings.Replace(identityFixture, `"is_stein_gate":0`, `"is_stein_gate":1`, 1), strings.Replace(identityFixture, `"title":"Fixture"`, `"redirect_url":"https://www.bilibili.com/bangumi/play/ep1"`, 1), strings.Replace(identityFixture, `"cid":222`, `"cid":0`, 1)} {
		if _, err := parseIdentity([]byte(raw), fixtureBV, 2); err == nil {
			t.Fatalf("malformed or unsupported identity accepted %s", raw)
		}
	}
	proxy, _ := NewAdapter(Config{ProxyURL: "http://proxy.internal:7890", HTTPClient: a.http})
	if _, err := proxy.ResolveIdentity(context.Background(), "https://www.bilibili.com/video/"+fixtureBV); !errors.Is(err, ErrHTTPProxyUnsupported) {
		t.Fatalf("proxy unsupported code=%v", err)
	}
}

func TestStructuredTrackProbeScopedConfigAndCleanup(t *testing.T) {
	var probeDirectory string
	a, err := NewAdapter(Config{YtDlpPath: "/trusted/yt-dlp", FFmpegPath: "/trusted/ffmpeg", CookiesPath: "/private/cookies.txt", ProxyURL: "http://trusted-proxy", HTTPClient: fixtureGetter(func(context.Context, string) ([]byte, string, error) {
		t.Fatal("inline subtitle must not fetch HTTP")
		return nil, "", nil
	}), Runner: func(_ context.Context, binary string, args []string) ([]byte, error) {
		if binary != "/trusted/yt-dlp" {
			t.Fatal("configured binary not used")
		}
		joined := strings.Join(args, " ")
		for _, expected := range []string{"--ignore-config", "--write-subs", "--skip-download", "--dump-single-json", "--sub-langs all,-danmaku", "--sub-format srt", "--cookies /private/cookies.txt", "--ffmpeg-location /trusted/ffmpeg", "--proxy http://trusted-proxy"} {
			if !strings.Contains(joined, expected) {
				t.Fatalf("missing %s in %v", expected, args)
			}
		}
		for i, arg := range args {
			if arg == "--paths" {
				probeDirectory = args[i+1]
			}
		}
		if _, err := os.Stat(probeDirectory); err != nil {
			t.Fatal("probe directory does not exist")
		}
		return []byte(subtitleFixture), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := a.ListSubtitleTracks(context.Background(), fixtureIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks include danmaku or lost subtitles: %+v", tracks)
	}
	if _, err := os.Stat(probeDirectory); !os.IsNotExist(err) {
		t.Fatal("probe directory leaked")
	}
	selected, err := SelectSubtitleTrack(tracks, "", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Language != "zh-Hans" || selected.SubtitleKind != "unknown" {
		t.Fatalf("wrong-language manual chosen or inferred manual from membership/status: %+v", selected)
	}
	raw, err := a.FetchSelectedSubtitle(context.Background(), fixtureIdentity(t), selected)
	if err != nil || !strings.Contains(string(raw), "字幕") {
		t.Fatalf("inline fetch=%s %v", raw, err)
	}
	public, _ := json.Marshal(selected)
	if strings.Contains(string(public), "00:00") || strings.Contains(string(public), "subtitle_url") {
		t.Fatalf("private payload exposed: %s", public)
	}
	selected.CID = 111
	if _, err := a.FetchSelectedSubtitle(context.Background(), fixtureIdentity(t), selected); err == nil {
		t.Fatal("accepted subtitle from another part")
	}
}

func TestTrackMetadataMismatchAndDanmakuOnly(t *testing.T) {
	id := fixtureIdentity(t)
	for _, raw := range []string{strings.Replace(subtitleFixture, "_p2", "_p1", 1), strings.Replace(subtitleFixture, `"duration":40`, `"duration":300`, 1), strings.Replace(subtitleFixture, `"id":`, `"_type":"playlist","id":`, 1), strings.Replace(subtitleFixture, "BiliBili", "OtherExtractor", 1)} {
		if _, err := parseSubtitleTracks([]byte(raw), id); err == nil {
			t.Fatal("accepted mismatched metadata")
		}
	}
	tracks, err := parseSubtitleTracks([]byte(`{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","subtitles":{"danmaku":[{"ext":"xml","url":"https://comment.bilibili.com/222.xml"}]}}`), id)
	if err != nil || len(tracks) != 0 {
		t.Fatalf("danmaku became subtitles: %+v %v", tracks, err)
	}
	if _, err := SelectSubtitleTrack(tracks, "", "zh"); err == nil {
		t.Fatal("danmaku-only video claims usable subtitles")
	}
}

func TestTrackSelectionKindsAndExplicitKey(t *testing.T) {
	tracks := []SubtitleTrack{{TrackKey: "en:1", Language: "en", SubtitleKind: "manual"}, {TrackKey: "zh:3", Language: "zh", SubtitleKind: "unknown"}, {TrackKey: "zh:2", Language: "zh", SubtitleKind: "automatic"}, {TrackKey: "zh:1", Language: "zh", SubtitleKind: "manual"}}
	selected, err := SelectSubtitleTrack(tracks, "", "zh")
	if err != nil || selected.TrackKey != "zh:1" {
		t.Fatalf("ranking=%+v %v", selected, err)
	}
	selected, err = SelectSubtitleTrack(tracks, "zh:3", "zh")
	if err != nil || selected.TrackKey != "zh:3" {
		t.Fatal("explicit user track not honored")
	}
	if _, err := SelectSubtitleTrack(tracks, "missing", "zh"); err == nil {
		t.Fatal("missing explicit track silently substituted")
	}
	if _, err := SelectSubtitleTrack(tracks, "", "ja"); err == nil {
		t.Fatal("wrong language silently substituted")
	}
}

func TestSubtitleURLPolicyAndSensitiveErrorRedaction(t *testing.T) {
	id := fixtureIdentity(t)
	a, _ := NewAdapter(Config{HTTPClient: fixtureGetter(func(_ context.Context, raw string) ([]byte, string, error) {
		return nil, "", errors.New("signed?signature=private cookie=secret")
	})})
	track := SubtitleTrack{TrackKey: "zh:1", Language: "zh", Format: "srt", BVID: id.BVID, CID: id.CID, PartIndex: id.PartIndex, downloadURL: "https://aisubtitle.hdslb.com/subtitle.srt?signature=private"}
	_, err := a.FetchSelectedSubtitle(context.Background(), id, track)
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "cookie") {
		t.Fatalf("provider secret leaked: %v", err)
	}
	track.downloadURL = "http://evil.test/subtitle.srt"
	if _, err := a.FetchSelectedSubtitle(context.Background(), id, track); err == nil {
		t.Fatal("external subtitle URL accepted")
	}
	proxy, _ := NewAdapter(Config{ProxyURL: "http://proxy", HTTPClient: a.http})
	track.downloadURL = "https://aisubtitle.hdslb.com/sub.srt"
	if _, err := proxy.FetchSelectedSubtitle(context.Background(), id, track); !errors.Is(err, ErrHTTPProxyUnsupported) {
		t.Fatalf("proxy gap not explicit: %v", err)
	}
}

func TestCookieJarDomainPathSecureAndExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	raw := "# Netscape HTTP Cookie File\n.bilibili.com\tTRUE\t/\tTRUE\t0\tSESSDATA\tprivate\nwww.bilibili.com\tFALSE\t/special\tFALSE\t0\tlocal\tvalue\n.evil.test\tTRUE\t/\tFALSE\t0\tsecret\tleak\n.bilibili.com\tTRUE\t/\tFALSE\t1\texpired\told\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	jar, err := loadCookieJar(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		raw   string
		count int
	}{{"https://api.bilibili.com/", 1}, {"http://api.bilibili.com/", 0}, {"https://www.bilibili.com/special/path", 2}, {"https://aisubtitle.hdslb.com/", 0}, {"https://evil.test/", 0}} {
		u, _ := url.Parse(test.raw)
		if got := len(jar.Cookies(u)); got != test.count {
			t.Fatalf("cookies for %s=%d want%d", test.raw, got, test.count)
		}
	}
}

func TestDownloadIdentityMetadataCIDMappingAndCleanup(t *testing.T) {
	for _, test := range []struct {
		name, metadata, after string
		success, provider     bool
	}{
		{"matching_provider", `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40,"requested_downloads":[{"url":"https://upos.bilivideo.com/upgcxcode/22/22/222/222-1-30080.m4s?signature=private"}]}`, identityFixture, true, true},
		{"mapping_only", `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40}`, identityFixture, true, false},
		{"wrong_download_part", `{"id":"BV1xx411c7mD_p1","extractor_key":"BiliBili","duration":40}`, identityFixture, false, false},
		{"wrong_stream_cid", `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40,"requested_formats":[{"url":"https://upos.bilivideo.com/upgcxcode/11/11/111/111-1-30080.m4s"}]}`, identityFixture, false, false},
		{"mapping_changed", `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40}`, strings.Replace(identityFixture, `"cid":222`, `"cid":333`, 1), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var directory string
			a, _ := NewAdapter(Config{HTTPClient: fixtureGetter(func(_ context.Context, raw string) ([]byte, string, error) { return []byte(test.after), raw, nil }), DownloadRunner: func(_ context.Context, _ string, args []string) error {
				for i, arg := range args {
					if arg == "-o" {
						directory = filepath.Dir(args[i+1])
					}
				}
				if !strings.Contains(strings.Join(args, " "), "--write-info-json") {
					t.Fatal("download metadata not saved")
				}
				if err := os.WriteFile(filepath.Join(directory, "media.info.json"), []byte(test.metadata), 0600); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(directory, "media.mp4"), []byte("fixture media bytes; not actual video"), 0600)
			}})
			video, err := a.DownloadVideoWithIdentity(context.Background(), fixtureIdentity(t))
			if (err == nil) != test.success {
				t.Fatalf("video=%+v err=%v", video, err)
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatal("temporary metadata directory leaked")
			}
			if test.success {
				defer os.Remove(video.Path)
				if video.ProviderCIDEvidence != test.provider || video.IdentityBasis == "" {
					t.Fatalf("identity proof=%+v", video)
				}
				if _, err := os.Stat(video.Path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProviderLanguageAliasesKeepTrackIdentityKindAndSelectionOrder(t *testing.T) {
	id := fixtureIdentity(t)
	raw := `{"id":"BV1xx411c7mD_p2","extractor_key":"BiliBili","duration":40,"subtitles":{"ai-zh":[{"ext":"srt","data":"字幕","ai_status":1}],"ai-en":[{"ext":"srt","data":"English"}],"auto-zh":[{"ext":"srt","data":"另一轨"}]}}`
	tracks, err := parseSubtitleTracks([]byte(raw), id)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := SelectSubtitleTrack(tracks, "", "zh-CN")
	if err != nil || selected.TrackKey != "ai-zh:1" || selected.Language != "ai-zh" || selected.SubtitleKind != "unknown" {
		t.Fatalf("selected=%+v %v", selected, err)
	}
	for _, track := range tracks {
		if track.SubtitleKind != "unknown" {
			t.Fatal("language prefix inferred subtitle type")
		}
	}
	if _, err := SelectSubtitleTrack([]SubtitleTrack{{TrackKey: "ai-en:1", Language: "ai-en", SubtitleKind: "unknown"}}, "", "zh-CN"); err == nil {
		t.Fatal("English AI track silently substituted for Chinese")
	}
	if _, err := SelectSubtitleTrack([]SubtitleTrack{{TrackKey: "ai-unknown:1", Language: "ai-unknown"}}, "", "zh-CN"); err == nil {
		t.Fatal("unknown AI track became Chinese")
	}
	selected, err = SelectSubtitleTrack(tracks, "ai-en:1", "zh-CN")
	if err != nil || selected.Language != "ai-en" {
		t.Fatal("explicit track key semantics changed")
	}
	tracks = append(tracks, SubtitleTrack{TrackKey: "zh-Hans:1", Language: "zh-Hans", SubtitleKind: "manual"})
	selected, err = SelectSubtitleTrack(tracks, "", "zh-CN")
	if err != nil || selected.TrackKey != "zh-Hans:1" {
		t.Fatal("manual/automatic/unknown ranking changed")
	}
	selected, err = SelectSubtitleTrack([]SubtitleTrack{{TrackKey: "ai-zh:1", Language: "ai-zh", SubtitleKind: "unknown"}}, "", "")
	if err != nil || selected.TrackKey != "ai-zh:1" {
		t.Fatal("default unspecified language rejected the only track")
	}
}
