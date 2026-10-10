package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"vid-lens/internal/pkg/remoteurl"
)

const maxMetadataBytes = 16 << 20
const maxSubtitleBytes = 5 << 20

// ErrHTTPProxyUnsupported lets the source workflow record a specific capability
// gap without treating a configured proxy as direct, verified network access.
var ErrHTTPProxyUnsupported = errors.New("validated HTTP probing does not support configured proxy egress")

// HTTPGetter is injectable for fixture tests. Production defaults to a direct,
// redirect-checked transport whose DNS check and connection use the same IP.
type HTTPGetter interface {
	Get(context.Context, string) ([]byte, string, error)
}

// Config is shared by metadata probes and downloads. A configured proxy still
// applies to external yt-dlp. New direct HTTP identity/subtitle requests fail
// closed with a proxy because ordinary CONNECT does not pin the checked target.
type Config struct {
	YtDlpPath      string
	FFmpegPath     string
	CookiesPath    string
	ProxyURL       string
	HTTPClient     HTTPGetter
	Runner         func(context.Context, string, []string) ([]byte, error)
	DownloadRunner func(context.Context, string, []string) error
}

type Adapter struct {
	config Config
	http   HTTPGetter
}

func NewAdapter(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.YtDlpPath) == "" {
		config.YtDlpPath = "yt-dlp"
	}
	if config.HTTPClient == nil {
		jar, err := loadCookieJar(config.CookiesPath)
		if err != nil {
			return nil, err
		}
		client, err := remoteurl.NewHTTPClient(remoteurl.HTTPOptions{AllowedHosts: []string{"bilibili.com", "b23.tv", "hdslb.com", "biliapi.net"}, CookieJar: jar})
		if err != nil {
			return nil, err
		}
		config.HTTPClient = client
	}
	if config.Runner == nil {
		config.Runner = runMetadataCommand
	}
	return &Adapter{config: config, http: config.HTTPClient}, nil
}

// BilibiliIdentity is resolved from official metadata; CID is never guessed
// from a URL, title, downloaded filename or selected subtitle wording.
type BilibiliIdentity struct {
	Platform        string    `json:"platform"`
	BVID            string    `json:"bvid"`
	AID             int64     `json:"aid"`
	CID             int64     `json:"cid"`
	PartIndex       int       `json:"part_index"`
	PartCount       int       `json:"part_count"`
	CanonicalURL    string    `json:"canonical_url"`
	DurationMS      int64     `json:"duration_ms"`
	Title           string    `json:"title"`
	IdentityVersion string    `json:"identity_version"`
	ResolvedAt      time.Time `json:"resolved_at"`
}

var regularVideoPath = regexp.MustCompile(`^/video/(BV[0-9A-Za-z]{10}|[aA][vV][1-9][0-9]*)(?:/)?$`)
var regularBV = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

// ResolveIdentity supports ordinary BV and AV videos plus validated b23
// redirects. AV is accepted only after official metadata confirms a BV ID.
// Collections, seasons, interactive videos and provider redirect targets fail.
func (a *Adapter) ResolveIdentity(ctx context.Context, rawURL string) (BilibiliIdentity, error) {
	if strings.TrimSpace(a.config.ProxyURL) != "" {
		return BilibiliIdentity{}, ErrHTTPProxyUnsupported
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Port() != "" && !((u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80"))) {
		return BilibiliIdentity{}, fmt.Errorf("invalid Bilibili video URL")
	}
	part, err := remoteurl.BilibiliPart(*u)
	if err != nil {
		return BilibiliIdentity{}, err
	}
	if remoteurl.NormalizeHost(u.Hostname()) == "b23.tv" {
		_, destination, err := a.http.Get(ctx, u.String())
		if err != nil {
			return BilibiliIdentity{}, fmt.Errorf("Bilibili short link resolution failed")
		}
		resolved, err := url.Parse(destination)
		if err != nil {
			return BilibiliIdentity{}, fmt.Errorf("invalid Bilibili short link target")
		}
		resolvedPart, err := remoteurl.BilibiliPart(*resolved)
		if err != nil {
			return BilibiliIdentity{}, err
		}
		if _, explicit := u.Query()["p"]; explicit {
			if _, targetExplicit := resolved.Query()["p"]; targetExplicit && resolvedPart != part {
				return BilibiliIdentity{}, fmt.Errorf("Bilibili short link part mismatch")
			}
			q := resolved.Query()
			q.Set("p", strconv.Itoa(part))
			resolved.RawQuery = q.Encode()
		} else {
			part = resolvedPart
		}
		u = resolved
	}
	host := remoteurl.NormalizeHost(u.Hostname())
	if host != "www.bilibili.com" && host != "bilibili.com" && host != "m.bilibili.com" {
		return BilibiliIdentity{}, fmt.Errorf("unsupported Bilibili video host")
	}
	path := regularVideoPath.FindStringSubmatch(u.Path)
	if path == nil {
		return BilibiliIdentity{}, fmt.Errorf("only ordinary BV/AV videos are supported")
	}
	query := url.Values{}
	if strings.HasPrefix(path[1], "BV") {
		query.Set("bvid", path[1])
	} else {
		query.Set("aid", path[1][2:])
	}
	data, _, err := a.http.Get(ctx, "https://api.bilibili.com/x/web-interface/view?"+query.Encode())
	if err != nil {
		return BilibiliIdentity{}, fmt.Errorf("Bilibili identity metadata request failed")
	}
	return parseIdentity(data, path[1], part)
}

func parseIdentity(raw []byte, requested string, part int) (BilibiliIdentity, error) {
	var response struct {
		Code int `json:"code"`
		Data struct {
			BVID        string `json:"bvid"`
			AID         int64  `json:"aid"`
			Title       string `json:"title"`
			RedirectURL string `json:"redirect_url"`
			Rights      struct {
				Interactive int `json:"is_stein_gate"`
			} `json:"rights"`
			Pages []struct {
				CID      int64       `json:"cid"`
				Page     int         `json:"page"`
				Duration json.Number `json:"duration"`
				Part     string      `json:"part"`
			} `json:"pages"`
		} `json:"data"`
	}
	if len(raw) > maxMetadataBytes || json.Unmarshal(raw, &response) != nil || response.Code != 0 {
		return BilibiliIdentity{}, fmt.Errorf("Bilibili identity metadata unavailable or malformed")
	}
	d := response.Data
	if !regularBV.MatchString(d.BVID) || d.AID <= 0 || d.Rights.Interactive != 0 || d.RedirectURL != "" {
		return BilibiliIdentity{}, fmt.Errorf("unsupported Bilibili media identity")
	}
	if strings.HasPrefix(requested, "BV") && requested != d.BVID {
		return BilibiliIdentity{}, fmt.Errorf("Bilibili metadata BV identity mismatch")
	}
	if !strings.HasPrefix(requested, "BV") {
		aid, err := strconv.ParseInt(requested[2:], 10, 64)
		if err != nil || aid != d.AID {
			return BilibiliIdentity{}, fmt.Errorf("Bilibili metadata AV identity mismatch")
		}
	}
	if part < 1 || part > len(d.Pages) || len(d.Pages) > 10000 {
		return BilibiliIdentity{}, fmt.Errorf("Bilibili part exceeds available pages")
	}
	seen := map[int64]bool{}
	for i, p := range d.Pages {
		if p.Page != i+1 || p.CID <= 0 || seen[p.CID] {
			return BilibiliIdentity{}, fmt.Errorf("Bilibili page identity malformed")
		}
		seen[p.CID] = true
	}
	p := d.Pages[part-1]
	duration, err := numberMS(p.Duration)
	if err != nil || duration <= 0 {
		return BilibiliIdentity{}, fmt.Errorf("Bilibili part duration unavailable")
	}
	canonical := "https://www.bilibili.com/video/" + d.BVID
	if part > 1 {
		canonical += "?p=" + strconv.Itoa(part)
	}
	title := d.Title
	if len(d.Pages) > 1 && p.Part != "" {
		title += " · " + p.Part
	}
	return BilibiliIdentity{Platform: "bilibili", BVID: d.BVID, AID: d.AID, CID: p.CID, PartIndex: part, PartCount: len(d.Pages), CanonicalURL: canonical, DurationMS: duration, Title: title, IdentityVersion: "bilibili-view-v1", ResolvedAt: time.Now().UTC()}, nil
}

func numberMS(n json.Number) (int64, error) {
	f, err := n.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f >= float64(math.MaxInt64)/1000 {
		return 0, fmt.Errorf("invalid duration")
	}
	return int64(math.Round(f * 1000)), nil
}

// SubtitleTrack is a server-only selected candidate. Private payload and signed
// URLs are excluded from serialization; APIs should expose its safe metadata.
type SubtitleTrack struct {
	TrackKey        string `json:"track_key"`
	ProviderTrackID string `json:"provider_track_id,omitempty"`
	Language        string `json:"language"`
	DisplayName     string `json:"display_name"`
	Format          string `json:"format"`
	SubtitleKind    string `json:"subtitle_kind"`
	KindBasis       string `json:"kind_basis"`
	BVID            string `json:"bvid"`
	CID             int64  `json:"cid"`
	PartIndex       int    `json:"part_index"`
	inlineData      string
	downloadURL     string
}

// ListSubtitleTracks uses structured yt-dlp JSON, never human --list-subs output.
// The installed Bilibili extractor returns inline SRT and omits kind evidence;
// membership in subtitles alone therefore remains explicitly unknown.
func (a *Adapter) ListSubtitleTracks(ctx context.Context, identity BilibiliIdentity) ([]SubtitleTrack, error) {
	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "vidlens-subs-")
	if err != nil {
		return nil, fmt.Errorf("create subtitle probe directory failed")
	}
	defer os.RemoveAll(directory)
	args := metadataArgs(a.config, identity.CanonicalURL, directory)
	raw, err := a.config.Runner(ctx, a.config.YtDlpPath, args)
	if err != nil {
		return nil, fmt.Errorf("subtitle metadata probe failed")
	}
	return parseSubtitleTracks(raw, identity)
}

func validateIdentity(id BilibiliIdentity) error {
	if id.Platform != "bilibili" || !regularBV.MatchString(id.BVID) || id.CID <= 0 || id.AID <= 0 || id.PartIndex < 1 || id.PartIndex > id.PartCount || id.DurationMS <= 0 {
		return fmt.Errorf("unresolved Bilibili identity")
	}
	u, err := url.Parse(id.CanonicalURL)
	if err != nil || u.Host != "www.bilibili.com" || u.Scheme != "https" || u.User != nil {
		return fmt.Errorf("invalid canonical Bilibili identity URL")
	}
	p, err := remoteurl.BilibiliPart(*u)
	if err != nil || p != id.PartIndex || u.Path != "/video/"+id.BVID {
		return fmt.Errorf("Bilibili canonical identity mismatch")
	}
	return nil
}

func metadataArgs(config Config, videoURL, directory string) []string {
	args := buildArgs(config.FFmpegPath, config.CookiesPath, config.ProxyURL, videoURL)
	// Move the URL after all explicit options; exec receives an argument array.
	args = args[:len(args)-1]
	args = append(args, "--skip-download", "--dump-single-json", "--write-subs", "--sub-langs", "all,-danmaku", "--sub-format", "srt", "--paths", directory, "--output", filepath.Join(directory, "%(id)s.%(ext)s"))
	return append(args, videoURL)
}

func parseSubtitleTracks(raw []byte, identity BilibiliIdentity) ([]SubtitleTrack, error) {
	var metadata struct {
		Type      string      `json:"_type"`
		ID        string      `json:"id"`
		Extractor string      `json:"extractor_key"`
		Duration  json.Number `json:"duration"`
		Subtitles map[string][]struct {
			Ext   string `json:"ext"`
			Data  string `json:"data"`
			URL   string `json:"url"`
			Name  string `json:"name"`
			ID    string `json:"id"`
			Kind  string `json:"subtitle_kind"`
			Basis string `json:"kind_basis"`
		} `json:"subtitles"`
	}
	if len(raw) > maxMetadataBytes || json.Unmarshal(raw, &metadata) != nil {
		return nil, fmt.Errorf("subtitle metadata malformed or oversized")
	}
	if metadata.Type != "" && metadata.Type != "video" {
		return nil, fmt.Errorf("unsupported playlist or interactive subtitle metadata")
	}
	if metadata.Extractor != "BiliBili" {
		return nil, fmt.Errorf("subtitle extractor identity mismatch")
	}
	expected := identity.BVID + "_p" + strconv.Itoa(identity.PartIndex)
	if metadata.ID != expected && !(identity.PartIndex == 1 && identity.PartCount == 1 && metadata.ID == identity.BVID) {
		return nil, fmt.Errorf("subtitle metadata video part mismatch")
	}
	if metadata.Duration != "" {
		duration, err := numberMS(metadata.Duration)
		if err != nil || duration <= 0 || abs(duration-identity.DurationMS) > 2000 {
			return nil, fmt.Errorf("subtitle metadata duration mismatch")
		}
	}
	keys := make([]string, 0, len(metadata.Subtitles))
	for language := range metadata.Subtitles {
		keys = append(keys, language)
	}
	sort.Strings(keys)
	tracks := []SubtitleTrack{}
	for _, language := range keys {
		if strings.EqualFold(language, "danmaku") || language == "" {
			continue
		}
		for index, entry := range metadata.Subtitles[language] {
			if strings.ToLower(entry.Ext) != "srt" || (entry.Data == "" && entry.URL == "") {
				continue
			}
			if len(entry.Data) > maxSubtitleBytes {
				return nil, fmt.Errorf("inline subtitle exceeds size limit")
			}
			kind, basis := "unknown", "yt-dlp metadata contains no confirmed subtitle type"
			if (entry.Kind == "manual" || entry.Kind == "automatic") && strings.TrimSpace(entry.Basis) != "" {
				kind, basis = entry.Kind, entry.Basis
			}
			name := entry.Name
			if name == "" {
				name = language
			}
			tracks = append(tracks, SubtitleTrack{TrackKey: language + ":" + strconv.Itoa(index+1), ProviderTrackID: entry.ID, Language: language, DisplayName: name, Format: "srt", SubtitleKind: kind, KindBasis: basis, BVID: identity.BVID, CID: identity.CID, PartIndex: identity.PartIndex, inlineData: entry.Data, downloadURL: entry.URL})
		}
	}
	return tracks, nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// SelectSubtitleTrack honors an explicitly available key first. Otherwise it
// ranks only language-matching tracks by evidenced manual, automatic, unknown.
// An unavailable explicit key returns an error rather than silently substituting.
func SelectSubtitleTrack(tracks []SubtitleTrack, explicitKey, preferredLanguage string) (SubtitleTrack, error) {
	if explicitKey != "" {
		for _, track := range tracks {
			if track.TrackKey == explicitKey {
				return track, nil
			}
		}
		return SubtitleTrack{}, fmt.Errorf("requested subtitle track unavailable")
	}
	rank := func(kind string) int {
		switch kind {
		case "manual":
			return 0
		case "automatic":
			return 1
		default:
			return 2
		}
	}
	candidates := []SubtitleTrack{}
	for _, track := range tracks {
		if preferredLanguage == "" || languageMatches(track.Language, preferredLanguage) {
			candidates = append(candidates, track)
		}
	}
	if len(candidates) == 0 {
		return SubtitleTrack{}, fmt.Errorf("no matching subtitle track")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if rank(candidates[i].SubtitleKind) != rank(candidates[j].SubtitleKind) {
			return rank(candidates[i].SubtitleKind) < rank(candidates[j].SubtitleKind)
		}
		return candidates[i].TrackKey < candidates[j].TrackKey
	})
	return candidates[0], nil
}

func languageMatches(actual, wanted string) bool {
	base := func(s string) string {
		return strings.SplitN(strings.ToLower(strings.ReplaceAll(s, "_", "-")), "-", 2)[0]
	}
	return base(actual) == base(wanted)
}

// FetchSelectedSubtitle accepts only a track bound to the frozen media identity.
// Inline data is preferable. URL-only SRT is fetched with the constrained HTTP
// client, without accepting downloader-supplied HTTP headers or arbitrary paths.
func (a *Adapter) FetchSelectedSubtitle(ctx context.Context, identity BilibiliIdentity, track SubtitleTrack) ([]byte, error) {
	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	if track.BVID != identity.BVID || track.CID != identity.CID || track.PartIndex != identity.PartIndex || track.Format != "srt" || track.TrackKey == "" || strings.EqualFold(track.Language, "danmaku") {
		return nil, fmt.Errorf("selected subtitle identity mismatch")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if track.inlineData != "" {
		if len(track.inlineData) > maxSubtitleBytes {
			return nil, fmt.Errorf("subtitle exceeds size limit")
		}
		return []byte(track.inlineData), nil
	}
	if strings.TrimSpace(a.config.ProxyURL) != "" {
		return nil, ErrHTTPProxyUnsupported
	}
	u, err := url.Parse(track.downloadURL)
	if err != nil || !remoteurl.HostAllowed(u.Hostname(), []string{"bilibili.com", "hdslb.com", "biliapi.net"}) {
		return nil, fmt.Errorf("subtitle download host rejected")
	}
	raw, _, err := a.http.Get(ctx, track.downloadURL)
	if err != nil {
		return nil, fmt.Errorf("subtitle download failed")
	}
	if len(raw) > maxSubtitleBytes {
		return nil, fmt.Errorf("subtitle exceeds size limit")
	}
	return raw, nil
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, fmt.Errorf("command output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func runMetadataCommand(ctx context.Context, binary string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	stdout := &boundedBuffer{max: maxMetadataBytes}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp metadata command failed")
	}
	return stdout.Bytes(), nil
}

func loadCookieJar(path string) (http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar creation failed")
	}
	if strings.TrimSpace(path) == "" {
		return jar, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Bilibili cookie file unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("Bilibili cookie file oversized or unreadable")
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return nil, fmt.Errorf("Bilibili cookie file malformed")
		}
		domain := strings.TrimPrefix(fields[0], ".")
		if !remoteurl.HostAllowed(domain, []string{"bilibili.com", "b23.tv", "hdslb.com", "biliapi.net"}) {
			continue
		}
		expires, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Bilibili cookie expiry malformed")
		}
		cookie := &http.Cookie{Name: fields[5], Value: fields[6], Path: fields[2], Secure: strings.EqualFold(fields[3], "TRUE")}
		if strings.EqualFold(fields[1], "TRUE") {
			cookie.Domain = fields[0]
		}
		if expires > 0 {
			cookie.Expires = time.Unix(expires, 0)
		}
		if cookie.Valid() != nil {
			return nil, fmt.Errorf("Bilibili cookie value malformed")
		}
		jar.SetCookies(&url.URL{Scheme: "https", Host: domain}, []*http.Cookie{cookie})
	}
	return jar, nil
}
