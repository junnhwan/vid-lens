package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
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

func youtubeSource(source string) bool {
	u, err := url.Parse(source)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be"
}

func (s *Service) ytdlpArgs(source string) []string {
	a := []string{"--ignore-config", "--no-cache-dir", "--no-playlist", "--no-warnings", "--no-progress", "--socket-timeout", "10", "--retries", "1", "--fragment-retries", "1", "--concurrent-fragments", "1", "--fixup", "never"}
	a = append(a, "--no-cookies", "--no-cookies-from-browser")
	if youtubeSource(source) {
		// Public YouTube work must never borrow the owner's account, even when
		// the shared platform file also contains YouTube login cookies.
		a = append(a, "--js-runtimes", "node")
	}
	if s.Config.ProxyURL != "" {
		a = append(a, "--proxy", s.Config.ProxyURL)
	}
	return a
}
func (s *Service) Prepare(ctx context.Context, c domain.Claim, p config.Profile) error {
	ctx, cancelPrepare := context.WithCancel(ctx)
	var audioDone chan struct{}
	var prefetchedAudio string
	var audioErr error
	var stopAudio context.CancelFunc
	defer func() {
		cancelPrepare()
		if audioDone != nil {
			<-audioDone
		}
	}()
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
	var bili *bilibiliSource
	var biliReader *bilibiliClient
	subtitleStatus := "platform_unspecified"
	origin := "platform_subtitles"
	coverage := "unknown"
	var language *string
	decision := subtitleDecision{Revision: "subtitle-adoption-v2", Candidates: []subtitleChoice{}, Attempts: []subtitleAttempt{}, FallbackReason: "not_listed"}
	restore := c.Run.Kind == "source.restore"
	step := domain.Step{ID: domain.ID(), Phase: "prepare_source", Title: "正在准备视频和文字来源", Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
	step, e = s.Store.Step(ctx, c, step)
	if e != nil {
		return e
	}
	advance := func(title string) error {
		step.Status = "completed"
		step.FinishedAt = domain.Ptr(time.Now().UTC())
		step.Outcome = &domain.Outcome{Message: "这一步来源准备已完成。", EvidenceStatus: "not_assessed"}
		if _, err := s.Store.Step(ctx, c, step); err != nil {
			return err
		}
		step = domain.Step{ID: domain.ID(), Phase: "prepare_source", Title: title, Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
		var err error
		step, err = s.Store.Step(ctx, c, step)
		return err
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
		if e = s.Resources.Reserve(ctx, c, min(p.WorkspaceBytes, st.Size()+64<<20), store.IncomingFrameBytes); e != nil {
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
	} else if ident.Platform == "bilibili" {
		if e = s.Resources.Reserve(ctx, c, p.WorkspaceBytes, store.IncomingFrameBytes); e != nil {
			return e
		}
		mctx, release, err := s.Resources.Acquire(ctx, c, "download")
		if err != nil {
			return err
		}
		biliReader = s.bilibiliReader()
		defer biliReader.http.CloseIdleConnections()
		subctx, cancel := context.WithTimeout(mctx, 30*time.Second)
		info, err := biliReader.source(subctx, ident)
		cancel()
		release()
		if err != nil {
			return err
		}
		bili, meta = &info, info.Meta
		if e = s.sourceMetadata(ctx, c, meta); e != nil {
			return e
		}
		if meta.Duration*1000 > float64(p.MaxVideoMS) {
			return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频超过时长限制。")
		}
		if !restore {
			dctx, rel, err := s.Resources.Acquire(ctx, c, "download")
			if err != nil {
				return err
			}
			subctx, cancel := context.WithTimeout(dctx, 30*time.Second)
			segs, language, subtitleStatus, decision = biliReader.subtitleDecision(subctx, info)
			cancel()
			rel()
			if e = s.Store.SourceDiagnostic(ctx, c, "subtitle-selection", decision); e != nil {
				return e
			}
		}
		dctx, rel, err := s.Resources.Acquire(ctx, c, "download")
		if err != nil {
			return err
		}
		subctx, cancel = context.WithTimeout(dctx, 20*time.Minute)
		video = filepath.Join(work, "video.input")
		err = biliReader.download(subctx, info.Video, video, p.MaxDownloadBytes)
		cancel()
		rel()
		if err != nil {
			return err
		}
	} else {
		if e = s.Resources.Reserve(ctx, c, p.WorkspaceBytes, store.IncomingFrameBytes); e != nil {
			return e
		}
		mctx, release, e := s.Resources.Acquire(ctx, c, "download")
		if e != nil {
			return e
		}
		subctx, cancel := context.WithTimeout(mctx, 30*time.Second)
		a := append(s.ytdlpArgs(ident.URL), "--dump-single-json", "--skip-download", ident.URL)
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
		if e = s.sourceMetadata(ctx, c, meta); e != nil {
			return e
		}
		s.Resources.Cache(ctx, "metadata:"+ident.URL, map[string]any{"title": meta.Title, "duration": meta.Duration})
		if e = advance("正在检查可用的平台字幕"); e != nil {
			return e
		}
		if !restore {
			infoFile := filepath.Join(work, "subtitle-info.json")
			if e = os.WriteFile(infoFile, raw, 0600); e != nil {
				return e
			}
			segs, language, decision, e = s.extractorSubtitles(ctx, c, meta, infoFile, work)
			_ = os.Remove(infoFile)
			if e != nil {
				return e
			}
		}
		if usable, _ := TranscriptCoverage(segs, int64(meta.Duration*1000)); !restore && !usable && ident.Platform == "youtube" {
			prefetchedAudio = filepath.Join(work, "source-audio.input")
			audioDone = make(chan struct{})
			audioCtx, cancelAudio := context.WithCancel(ctx)
			stopAudio = cancelAudio
			go func() {
				defer close(audioDone)
				audioStep := domain.Step{ID: "audio_" + c.Run.ID, Phase: "prepare_source", Title: "正在获取转写所需音轨", Status: "running", StartedAt: domain.Ptr(time.Now().UTC()), FrameIDs: []string{}}
				if audioStep, audioErr = s.Store.Step(ctx, c, audioStep); audioErr != nil {
					return
				}
				defer func() {
					audioStep.FinishedAt = domain.Ptr(time.Now().UTC())
					audioStep.Status = "completed"
					if audioErr != nil {
						audioStep.Status = "failed"
					}
					if audioCtx.Err() != nil {
						audioStep.Status = "canceled"
					}
					_, saveErr := s.Store.Step(ctx, c, audioStep)
					if audioErr == nil {
						audioErr = saveErr
					}
				}()
				dctx, release, err := s.Resources.Acquire(audioCtx, c, "download:audio")
				if err != nil {
					audioErr = err
					return
				}
				defer release()
				sub, cc := context.WithTimeout(dctx, 20*time.Minute)
				defer cc()
				audioErr = s.downloadStream(sub, ident.URL, "bestaudio[ext=m4a]/bestaudio/best", prefetchedAudio, min(p.MaxDownloadBytes, 64<<20))
			}()
		}
		if e = advance("正在获取用于观察的完整视频"); e != nil {
			return e
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
	if e = advance("正在校验视频时长与素材"); e != nil {
		return e
	}
	probe, e := s.Probe(ctx, c, video)
	if e != nil {
		return e
	}
	duration := probe.DurationMS
	if bili != nil && (float64(duration)+2000 < meta.Duration*1000 || probe.Width != bili.Video.Width || probe.Height != bili.Video.Height) {
		return domain.Err(422, "SOURCE_UNAVAILABLE", "B 站下载结果与完整视频或选定清晰度不一致。")
	}
	if duration <= 0 || duration > p.MaxVideoMS {
		return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频时长无效或超过限制。")
	}
	if meta.Title == "" {
		meta.Title = ident.CanonicalID
	}
	usable, cov := TranscriptCoverage(segs, duration)
	if stopAudio != nil && (usable || probe.HasAudio) {
		stopAudio()
		<-audioDone
	}
	decision.Usable = usable
	decision.Coverage = cov
	decision.Segments = len(segs)
	decision.DurationMS = duration
	decision.CoveredMS = subtitleCoveredMS(segs, duration)
	decision.CoverageRule = "cue-duration union: >=50% usable, >=85% complete; speech/silence not inferred"
	if !usable && len(segs) > 0 {
		decision.FallbackReason = "coverage_below_threshold"
	}
	if usable {
		decision.FallbackReason = ""
	}
	if restore {
		decision.FallbackReason = "restore_preserves_original_text"
	}
	if e = s.Store.SourceDiagnostic(ctx, c, "subtitle-coverage", decision); e != nil {
		return e
	}
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
		language = nil // Rejected subtitle language is not an ASR detection result.
		audioInput := video
		if ident.Platform != "local" && !probe.HasAudio {
			if audioDone != nil {
				select {
				case <-audioDone:
				case <-ctx.Done():
					return ctx.Err()
				}
				if audioErr != nil {
					return audioErr
				}
				st, err := os.Stat(video)
				if err != nil {
					return err
				}
				audioStat, err := os.Stat(prefetchedAudio)
				if err != nil {
					return err
				}
				if st.Size()+audioStat.Size() > p.MaxDownloadBytes {
					return domain.Err(422, "INPUT_LIMIT_EXCEEDED", "视频与音频合计超过下载限制。")
				}
				audioInput = prefetchedAudio
			} else {
				dctx, rel, e := s.Resources.Acquire(ctx, c, "download")
				if e != nil {
					return e
				}
				subctx, cancel := context.WithTimeout(dctx, 20*time.Minute)
				audioInput = filepath.Join(work, "source-audio.input")
				st, _ := os.Stat(video)
				left := p.MaxDownloadBytes - st.Size()
				if bili != nil {
					e = biliReader.download(subctx, bili.Audio, audioInput, left)
				} else {
					e = s.downloadStream(subctx, ident.URL, "bestaudio[ext=m4a]/bestaudio/best", audioInput, left)
				}
				cancel()
				rel()
				if e != nil {
					return e
				}
			}
		}
		if e = advance("正在转写缺少可用字幕的视频"); e != nil {
			return e
		}
		step.Title = "正在转写缺少可用字幕的视频（最多并行2个音频窗口）"
		if step, e = s.Store.Step(ctx, c, step); e != nil {
			return e
		}
		segs, e = s.Transcribe(ctx, c, p, audioInput, duration, work, func(completed int64) error {
			step.Title = fmt.Sprintf("正在转写音频（%s / %s）", mediaClock(completed), mediaClock(duration))
			var err error
			step, err = s.Store.Step(ctx, c, step)
			return err
		})
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
	if stopAudio != nil {
		stopAudio()
		<-audioDone
	}
	decision.AdoptedOrigin = origin
	if e = s.Store.SourceDiagnostic(ctx, c, "text-adopted", decision); e != nil {
		return e
	}
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
	metadata["subtitle_decision"] = decision
	if decision.Selected != nil {
		metadata["subtitle_kind"] = decision.Selected.Kind
	}
	if ident.Platform == "youtube" || ident.Platform == "bilibili" {
		metadata["access_policy"] = "anonymous"
	}
	if bili != nil {
		metadata["extractor"] = "bilibili_public_api_v1"
		metadata["subtitle_availability"] = subtitleStatus
		metadata["available_video_heights"] = bili.Heights
		metadata["selected_quality"] = map[string]any{"width": bili.Video.Width, "height": bili.Video.Height, "quality_id": bili.Video.ID, "frame_rate": bili.Video.FrameRate, "bandwidth": bili.Video.Bandwidth}
	}
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

func mediaClock(ms int64) string {
	seconds := (ms + 999) / 1000
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
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
	if choices := subtitleChoices(m); len(choices) > 0 {
		return choices[0].Language, choices[0].Kind == "automatic"
	}
	return "", false
}

func (s *Service) sourceMetadata(ctx context.Context, c domain.Claim, m metadata) error {
	if _, err := s.Store.BeginOperation(ctx, c, "source-metadata", "source_metadata", map[string]any{"source_id": c.Run.SourceID}, false); err != nil {
		return err
	}
	if err := s.Store.FinishOperation(ctx, c, "source-metadata", map[string]any{"title": m.Title, "duration_ms": int64(m.Duration * 1000)}, "", nil); err != nil {
		return err
	}
	return s.Store.NotifySourceMetadata(ctx, c)
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
