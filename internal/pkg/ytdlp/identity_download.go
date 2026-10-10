package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"vid-lens/internal/pkg/remoteurl"
)

type DownloadedVideo struct {
	Path     string
	Identity BilibiliIdentity
	// IdentityBasis records the evidence actually available. The installed
	// extractor lacks a top-level cid, so BV+part metadata plus unchanged
	// official page mapping is the minimum accepted proof. Selected stream
	// URLs are checked when their provider path exposes an actual CID.
	IdentityBasis       string
	ProviderCIDEvidence bool
}

// DownloadVideoWithIdentity preserves legacy DownloadVideo while providing a
// stronger Bilibili path: official identity before download, actual saved
// extraction metadata, selected-stream CID if available, and a post-download
// official mapping check. It does not claim external yt-dlp egress is sandboxed.
// The caller owns the returned media file; every sidecar/temporary directory is
// removed on success and failure. The caller computes the media fingerprint.
func (a *Adapter) DownloadVideoWithIdentity(ctx context.Context, identity BilibiliIdentity) (DownloadedVideo, error) {
	if strings.TrimSpace(a.config.ProxyURL) != "" {
		return DownloadedVideo{}, ErrHTTPProxyUnsupported
	}
	if err := validateIdentity(identity); err != nil {
		return DownloadedVideo{}, err
	}
	directory, err := os.MkdirTemp("", "vidlens-download-")
	if err != nil {
		return DownloadedVideo{}, fmt.Errorf("create video download directory failed")
	}
	defer os.RemoveAll(directory)
	args := buildArgs(a.config.FFmpegPath, a.config.CookiesPath, a.config.ProxyURL, identity.CanonicalURL)
	args = args[:len(args)-1]
	args = append(args, "--write-info-json", "-o", filepath.Join(directory, "media.%(ext)s"), identity.CanonicalURL)
	if a.config.DownloadRunner != nil {
		err = a.config.DownloadRunner(ctx, a.config.YtDlpPath, args)
	} else {
		cmd := exec.CommandContext(ctx, a.config.YtDlpPath, args...)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		err = cmd.Run()
	}
	if err != nil {
		return DownloadedVideo{}, fmt.Errorf("video download failed")
	}
	metadataFile, err := os.Open(filepath.Join(directory, "media.info.json"))
	if err != nil {
		return DownloadedVideo{}, fmt.Errorf("download identity metadata missing")
	}
	raw, err := io.ReadAll(io.LimitReader(metadataFile, maxMetadataBytes+1))
	metadataFile.Close()
	if err != nil || len(raw) > maxMetadataBytes {
		return DownloadedVideo{}, fmt.Errorf("download identity metadata oversized or unreadable")
	}
	if _, err := parseSubtitleTracks(raw, identity); err != nil {
		return DownloadedVideo{}, err
	}
	cidEvidence, err := verifySelectedCIDs(raw, identity.CID)
	if err != nil {
		return DownloadedVideo{}, err
	}
	after, err := a.ResolveIdentity(ctx, identity.CanonicalURL)
	if err != nil {
		return DownloadedVideo{}, err
	}
	if after.BVID != identity.BVID || after.AID != identity.AID || after.CID != identity.CID || after.PartIndex != identity.PartIndex || after.DurationMS != identity.DurationMS {
		return DownloadedVideo{}, fmt.Errorf("Bilibili identity changed during download")
	}
	mediaPath := filepath.Join(directory, "media.mp4")
	info, err := os.Stat(mediaPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return DownloadedVideo{}, fmt.Errorf("downloaded video file missing or empty")
	}
	outputPath := filepath.Join(os.TempDir(), uuid.NewString()+".mp4")
	if err := os.Rename(mediaPath, outputPath); err != nil {
		return DownloadedVideo{}, fmt.Errorf("preserve downloaded video failed")
	}
	basis := "yt-dlp extracted BV+part matched official page CID before and after download"
	if cidEvidence {
		basis += "; selected provider stream CID verified"
	}
	return DownloadedVideo{Path: outputPath, Identity: after, IdentityBasis: basis, ProviderCIDEvidence: cidEvidence}, nil
}

func verifySelectedCIDs(raw []byte, expectedCID int64) (bool, error) {
	type stream struct {
		URL              string `json:"url"`
		RequestedFormats []struct {
			URL string `json:"url"`
		} `json:"requested_formats"`
	}
	var metadata struct {
		CID                int64    `json:"cid"`
		RequestedDownloads []stream `json:"requested_downloads"`
		RequestedFormats   []struct {
			URL string `json:"url"`
		} `json:"requested_formats"`
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &metadata) != nil {
		return false, fmt.Errorf("download metadata malformed")
	}
	evidence := false
	if metadata.CID > 0 {
		if metadata.CID != expectedCID {
			return false, fmt.Errorf("download metadata CID mismatch")
		}
		evidence = true
	}
	urls := []string{}
	for _, download := range metadata.RequestedDownloads {
		if download.URL != "" {
			urls = append(urls, download.URL)
		}
		for _, format := range download.RequestedFormats {
			urls = append(urls, format.URL)
		}
	}
	for _, format := range metadata.RequestedFormats {
		urls = append(urls, format.URL)
	}
	if len(urls) == 0 && metadata.URL != "" {
		urls = append(urls, metadata.URL)
	}
	for _, rawURL := range urls {
		if cid, ok := providerStreamCID(rawURL); ok {
			if cid != expectedCID {
				return false, fmt.Errorf("selected provider stream CID mismatch")
			}
			evidence = true
		}
	}
	return evidence, nil
}

// Bilibili's regular upgcxcode stream paths identify the CID both in the final
// directory and the filename prefix. Other URL shapes are not guessed.
func providerStreamCID(rawURL string) (int64, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || !remoteurl.HostAllowed(u.Hostname(), []string{"bilivideo.com", "bilivideo.cn", "hdslb.com"}) {
		return 0, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "upgcxcode" {
		return 0, false
	}
	directory := parts[len(parts)-2]
	prefix := strings.SplitN(parts[len(parts)-1], "-", 2)
	if len(prefix) != 2 || prefix[0] != directory {
		return 0, false
	}
	cid, err := strconv.ParseInt(directory, 10, 64)
	return cid, err == nil && cid > 0
}
