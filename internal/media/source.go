package media

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"vidlens/internal/config"
	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type metadata struct {
	ID         string                 `json:"id"`
	Title      string                 `json:"title"`
	Duration   float64                `json:"duration"`
	IsLive     bool                   `json:"is_live"`
	LiveStatus string                 `json:"live_status"`
	Type       string                 `json:"_type"`
	Subtitles  map[string][]subformat `json:"subtitles"`
	Auto       map[string][]subformat `json:"automatic_captions"`
	Entries    []metadata             `json:"entries"`
}
type subformat struct {
	Ext string `json:"ext"`
}

func (s *Service) ytdlpArgs() []string {
	a := []string{"--ignore-config", "--no-cache-dir", "--no-playlist", "--no-warnings", "--no-progress", "--socket-timeout", "10", "--retries", "1", "--fragment-retries", "1", "--concurrent-fragments", "1", "--fixup", "never"}
	if s.Config.CookieFile != "" {
		a = append(a, "--cookies", s.Config.CookieFile)
	}
	if s.Config.ProxyURL != "" {
		a = append(a, "--proxy", s.Config.ProxyURL)
	}
	return a
}
func (s *Service) Prepare(ctx context.Context, c domain.Claim, p config.Profile) error {
	ident, e := s.Store.SourceIdentity(ctx, c.Run.SourceID)
	if e != nil {
		return e
	}
	work, e := s.work(c)
	if e != nil {
		return e
	}
	var meta metadata
	var video string
	var segs []domain.Segment
	origin := "platform_subtitles"
	coverage := "unknown"
	var language *string
	restore := c.Run.Kind == "source.restore"
	step := domain.Step{ID: domain.ID(), Phase: "prepare_source", Title: "正在准备视频和文字来源", Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
	step, e = s.Store.Step(ctx, c, step)
	if e != nil {
		return e
	}
	if ident.Platform == "local" {
		src, e := s.localFile(ident.CanonicalID)
		if e != nil {
			return domain.Err(422, "SOURCE_UNAVAILABLE", "开发样本不存在或无法读取。")
		}
		st, e := os.Stat(src)
		if e != nil {
			return domain.Err(422, "SOURCE_UNAVAILABLE", "开发样本不存在。")
		}
		if st.Size() > p.MaxDownloadBytes {
			return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频超过大小限制。")
		}
		if e = s.Resources.Reserve(ctx, c, min(p.WorkspaceBytes, st.Size()+64<<20), 20<<20); e != nil {
			return e
		}
		dst := filepath.Join(work, "video.mp4")
		f, e := os.Open(src)
		if e != nil {
			return e
		}
		_, _, e = copyBounded(ctx, dst, f, p.MaxDownloadBytes)
		f.Close()
		if e != nil {
			return e
		}
		video = dst
		meta.Title = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		if !restore {
			for _, ext := range []string{".vtt", ".srt", ".json"} {
				b, e := os.ReadFile(strings.TrimSuffix(src, filepath.Ext(src)) + ext)
				if e == nil {
					if len(b) > 16<<20 {
						return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "字幕过大。")
					}
					segs, e = ParseSubtitles(b, ext)
					if e != nil {
						return e
					}
					break
				}
			}
		}
	} else {
		if e = s.Resources.Reserve(ctx, c, p.WorkspaceBytes, 20<<20); e != nil {
			return e
		}
		mctx, release, e := s.Resources.Acquire(ctx, c, "download")
		if e != nil {
			return e
		}
		subctx, cancel := context.WithTimeout(mctx, 30*time.Second)
		a := append(s.ytdlpArgs(), "--dump-single-json", "--skip-download", ident.URL)
		raw, e := command(subctx, "yt-dlp", a, "", 0, 0)
		cancel()
		release()
		if e != nil {
			return e
		}
		if json.Unmarshal(raw, &meta) != nil {
			return errors.New("invalid extractor response")
		}
		if meta.Type == "playlist" {
			if len(meta.Entries) != 1 {
				return domain.Err(422, "UNSUPPORTED_SOURCE", "首版只接受单个视频或分 P。")
			}
			meta = meta.Entries[0]
		}
		if meta.IsLive || meta.LiveStatus == "is_live" || meta.LiveStatus == "is_upcoming" {
			return domain.Err(422, "UNSUPPORTED_SOURCE", "暂不处理直播来源。")
		}
		if meta.Duration*1000 > float64(p.MaxVideoMS) {
			return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频超过时长限制。")
		}
		s.Resources.Cache(ctx, "metadata:"+ident.URL, map[string]any{"title": meta.Title, "duration": meta.Duration})
		if !restore {
			lang, auto := chooseSubtitle(meta)
			if lang != "" {
				dctx, rel, e := s.Resources.Acquire(ctx, c, "download")
				if e != nil {
					return e
				}
				subctx, cancel := context.WithTimeout(dctx, 60*time.Second)
				a := append(s.ytdlpArgs(), "--skip-download", "--sub-langs", lang, "--sub-format", "json3/vtt/best", "-o", filepath.Join(work, "sub.%(ext)s"))
				if auto {
					a = append(a, "--write-auto-subs")
				} else {
					a = append(a, "--write-subs")
				}
				a = append(a, ident.URL)
				_, suberr := command(subctx, "yt-dlp", a, work, 16<<20, 0)
				cancel()
				rel()
				if suberr == nil {
					matches, _ := filepath.Glob(filepath.Join(work, "sub.*"))
					for _, file := range matches {
						b, e := os.ReadFile(file)
						if e == nil && len(b) <= 16<<20 {
							segs, _ = ParseSubtitles(b, filepath.Ext(file))
							if len(segs) > 0 {
								language = &lang
								break
							}
						}
					}
				}
			}
		}
		dctx, rel, e := s.Resources.Acquire(ctx, c, "download")
		if e != nil {
			return e
		}
		subctx, cancel = context.WithTimeout(dctx, 20*time.Minute)
		video = filepath.Join(work, "video.input")
		format := "bestvideo[height<=1080][ext=mp4]/bestvideo[height<=1080]/best[height<=1080]"
		e = s.downloadStream(subctx, ident.URL, format, video, p.MaxDownloadBytes)
		cancel()
		rel()
		if e != nil {
			return e
		}
	}
	probe, e := s.Probe(ctx, c, video)
	if e != nil {
		return e
	}
	duration := probe.DurationMS
	if duration <= 0 || duration > p.MaxVideoMS {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频时长无效或超过限制。")
	}
	if meta.Title == "" {
		meta.Title = ident.CanonicalID
	}
	usable, cov := TranscriptCoverage(segs, duration)
	coverage = cov
	asrReused := false
	var reusedSnapshot string
	// This fingerprint can justify ASR reuse only when it includes the audio
	// actually transcribed. Separate remote audio tracks must still be acquired.
	if !restore && !usable && probe.HasAudio {
		hash, e := fingerprintFile(ctx, video)
		if e != nil {
			return e
		}
		prior, found, e := s.Store.ReusableASR(ctx, c.Run.SourceID, hash, asrPipelineVersion, duration, p)
		if e != nil {
			return e
		}
		if found {
			segs, e = s.Store.Transcript(ctx, prior.ID)
			if e != nil {
				return e
			}
			if len(segs) > 0 {
				asrReused = true
				reusedSnapshot = prior.ID
				origin = "asr"
				_, coverage = TranscriptCoverage(segs, duration)
				language = prior.Source.TextProvenance.Language
				step.Title = "正在复用已完整校验视频的转写"
				if step, e = s.Store.Step(ctx, c, step); e != nil {
					return e
				}
			}
		}
	}
	if !restore && !usable && !asrReused && (ident.Platform != "local" || probe.HasAudio) {
		origin = "asr"
		audioInput := video
		if ident.Platform != "local" && !probe.HasAudio {
			dctx, rel, e := s.Resources.Acquire(ctx, c, "download")
			if e != nil {
				return e
			}
			subctx, cancel := context.WithTimeout(dctx, 20*time.Minute)
			audioInput = filepath.Join(work, "source-audio.input")
			st, _ := os.Stat(video)
			left := p.MaxDownloadBytes - st.Size()
			e = s.downloadStream(subctx, ident.URL, "bestaudio[ext=m4a]/bestaudio/best", audioInput, left)
			cancel()
			rel()
			if e != nil {
				return e
			}
		}
		step.Title = "正在转写缺少可用字幕的视频"
		if step, e = s.Store.Step(ctx, c, step); e != nil {
			return e
		}
		segs, e = s.Transcribe(ctx, c, p, audioInput, duration, work)
		if e != nil {
			return e
		}
		_, coverage = TranscriptCoverage(segs, duration)
		if audioInput != video {
			_ = os.Remove(audioInput)
		}
		if len(segs) == 0 {
			origin = "unavailable"
			coverage = "unknown"
		}
	}
	if !restore && len(segs) == 0 {
		coverage = "unknown"
		origin = "unavailable"
	} // Silent/visual clips can still be investigated; publication must declare absent text.
	a, e := s.own(ctx, c, video, "video", "application/octet-stream", c.Run.SnapshotID, 24*time.Hour)
	if e != nil {
		return e
	}
	if e = s.Store.AddAsset(ctx, c, a); e != nil {
		return e
	}
	source := domain.Source{SourceID: c.Run.SourceID, Platform: ident.Platform, CanonicalURL: ident.URL, Title: &meta.Title, DurationMS: &duration, TextProvenance: &domain.Provenance{Origin: origin, Coverage: coverage, Language: language}}
	if ident.Platform == "bilibili" {
		source.Part = &domain.Part{Key: ident.PartKey, Label: "第 " + ident.PartKey + " P"}
	}
	stable := []map[string]any{}
	for i := range segs {
		segs[i].Ordinal = i
		stable = append(stable, map[string]any{"start_ms": segs[i].StartMS, "end_ms": segs[i].EndMS, "text": segs[i].Text, "origin": segs[i].Origin})
	}
	metadata := map[string]any{"schema_version": 1, "extractor": "yt-dlp", "video": probe, "content_verification": "sha256_full_media", "subtitle_kind": "platform_unspecified"}
	if origin == "asr" {
		metadata["asr_pipeline_version"] = asrPipelineVersion
	}
	if reusedSnapshot != "" {
		metadata["reused_asr_snapshot_id"] = reusedSnapshot
	}
	snap := domain.Snapshot{ID: domain.ID(), SourceID: c.Run.SourceID, VideoHash: a.SHA256, TextHash: store.Hash(stable), DurationMS: duration, Source: source, Metadata: domain.JSON(metadata)}
	if restore {
		old, e := s.Store.Snapshot(ctx, *c.Run.SnapshotID)
		if e != nil {
			return e
		}
		snap.TextHash = old.TextHash
	}
	step.Status = "completed"
	step.FinishedAt = domain.Ptr(time.Now().UTC())
	step.Outcome = &domain.Outcome{Message: "视频及来源清单已校验，文字来源已记录。", EvidenceStatus: "not_applicable"}
	if _, e = s.Store.Step(ctx, c, step); e != nil {
		return e
	}
	_, e = s.Store.FinishSource(ctx, c, snap, segs, a.ID)
	return e
}
func (s *Service) localFile(name string) (string, error) {
	if s.Config.DevMediaDir == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", domain.Err(422, "UNSUPPORTED_SOURCE", "本地样本入口未启用。")
	}
	root, e := filepath.EvalSymlinks(s.Config.DevMediaDir)
	if e != nil {
		return "", e
	}
	p, e := filepath.EvalSymlinks(filepath.Join(root, name))
	if e != nil {
		return "", e
	}
	if !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return "", domain.Err(422, "UNSUPPORTED_SOURCE", "样本必须位于开发目录内。")
	}
	return p, nil
}
func chooseSubtitle(m metadata) (string, bool) {
	for _, entry := range []struct {
		Tracks map[string][]subformat
		Auto   bool
	}{{m.Subtitles, false}, {m.Auto, true}} {
		for _, want := range []string{"zh-Hans", "zh-CN", "zh", "en", "en-US"} {
			if len(entry.Tracks[want]) > 0 {
				return want, entry.Auto
			}
		}
		langs := []string{}
		for lang, formats := range entry.Tracks {
			if len(formats) > 0 {
				langs = append(langs, lang)
			}
		}
		slices.Sort(langs)
		for _, prefix := range []string{"zh", "en"} {
			for _, lang := range langs {
				if strings.HasPrefix(lang, prefix) {
					return lang, entry.Auto
				}
			}
		}
		if len(langs) > 0 {
			return langs[0], entry.Auto
		}
	}
	return "", false
}

type Probe struct {
	DurationMS int64 `json:"duration_ms"`
	Width      int   `json:"width"`
	Height     int   `json:"height"`
	HasAudio   bool  `json:"has_audio"`
}

func (s *Service) Probe(ctx context.Context, c domain.Claim, path string) (Probe, error) {
	fctx, release, e := s.Resources.Acquire(ctx, c, "ffmpeg")
	if e != nil {
		return Probe{}, e
	}
	defer release()
	sub, cancel := context.WithTimeout(fctx, 15*time.Second)
	defer cancel()
	raw, e := command(sub, "ffprobe", []string{"-v", "error", "-show_format", "-show_streams", "-of", "json", path}, "", 0, 0)
	if e != nil {
		return Probe{}, e
	}
	var v struct {
		Format struct {
			Duration string `json:"duration"`
		}
		Streams []struct {
			Type          string `json:"codec_type"`
			Width, Height int
		}
	}
	if json.Unmarshal(raw, &v) != nil {
		return Probe{}, errors.New("invalid probe output")
	}
	var dur float64
	_, _ = fmtSscan(v.Format.Duration, &dur)
	out := Probe{DurationMS: int64(math.Round(dur * 1000))}
	for _, st := range v.Streams {
		if st.Type == "video" {
			out.Width = st.Width
			out.Height = st.Height
		}
		if st.Type == "audio" {
			out.HasAudio = true
		}
	}
	if out.Width == 0 {
		return out, domain.Err(422, "SOURCE_UNAVAILABLE", "没有可解码的视频轨。")
	}
	return out, nil
}
