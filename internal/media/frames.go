package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

type Window struct {
	StartMS int64        `json:"start_ms"`
	EndMS   int64        `json:"end_ms"`
	Count   int          `json:"count"`
	Mode    string       `json:"mode"`
	Crop    *domain.Crop `json:"crop,omitempty"`
}
type Inspection struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"inspection_id"`
	Question      string         `json:"question"`
	FrameIDs      []string       `json:"frame_ids"`
	Frames        []domain.Frame `json:"frames"`
	Range         *domain.Range  `json:"range"`
	Sampling      string         `json:"sampling"`
}

var ptsTime = regexp.MustCompile(`pts_time:([-0-9.]+)`)

func (s *Service) Inspect(ctx context.Context, c domain.Claim, key, question string, w Window, reload []string) (Inspection, error) {
	result := Inspection{SchemaVersion: 1, ID: key, Question: question, FrameIDs: []string{}, Frames: []domain.Frame{}, Sampling: "bounded temporal samples; unseen events may remain"}
	if c.Run.SnapshotID == nil {
		return result, errors.New("snapshot missing")
	}
	if len(reload) > 0 {
		if len(reload) > 6 || w.EndMS > 0 || w.Crop != nil {
			return result, domain.Err(422, "INVALID_ARGUMENT", "重新加载与新窗口不能同时请求。")
		}
		for _, id := range reload {
			a, _, e := s.Reload(ctx, c, id, key)
			if e != nil {
				return result, e
			}
			result.FrameIDs = append(result.FrameIDs, a.ID)
		}
		result.Sampling = "reloaded original pixels"
		return result, nil
	}
	args := map[string]any{"window": w, "question": question}
	old, e := s.Store.BeginOperation(ctx, c, key, "inspect", args, false)
	if e != nil {
		return result, e
	}
	if old.Status == "completed" {
		if e = json.Unmarshal(old.Result, &result); e != nil {
			return result, e
		}
		for _, id := range result.FrameIDs {
			if _, _, e = s.ReadImage(ctx, c, id); e != nil {
				return result, e
			}
		}
		return result, nil
	}
	if w.Count == 0 {
		w.Count = 4
	}
	if w.Count < 1 || w.Count > 6 {
		return result, domain.Err(422, "INVALID_ARGUMENT", "单次最多提取六帧。")
	}
	snap, e := s.Store.Snapshot(ctx, *c.Run.SnapshotID)
	if e != nil {
		return result, e
	}
	if w.Crop == nil && (w.StartMS < 0 || w.EndMS <= w.StartMS || w.EndMS > snap.DurationMS || w.EndMS-w.StartMS > 60000) {
		return result, domain.Err(422, "INVALID_ARGUMENT", "窗口必须位于当前视频内且不超过六十秒。")
	}
	imageBudget := int64(20 << 20)
	if c.Run.Kind == "qa.answer" {
		imageBudget = 8 << 20
	}
	if e = s.Resources.Reserve(ctx, c, 0, imageBudget); e != nil {
		return result, e
	}
	work, e := s.work(c)
	if e != nil {
		return result, e
	}
	var input string
	var parent domain.Asset
	if w.Crop != nil {
		parent, _, e = s.ReadImage(ctx, c, w.Crop.ParentFrameID)
		if e != nil {
			if errors.Is(e, ErrMediaMissing) {
				return result, e
			}
			return result, domain.Err(422, "INVALID_ARGUMENT", "crop.parent_frame_id 必须是实际已交付的 frame_id，不能使用 inspection_id。")
		}
		if w.Crop.X < 0 || w.Crop.Y < 0 || w.Crop.Width <= 0 || w.Crop.Height <= 0 || w.Crop.X+w.Crop.Width > parent.Width || w.Crop.Y+w.Crop.Height > parent.Height {
			return result, domain.Err(422, "INVALID_ARGUMENT", "裁剪范围超出父图。")
		}
		input, _ = s.Path(parent.StorageKey)
		w.Count = 1
	} else {
		video, e := s.Store.Video(ctx, *c.Run.SnapshotID)
		if e != nil {
			return result, ErrMediaMissing
		}
		input, _ = s.Path(video.StorageKey)
		if _, e = os.Stat(input); e != nil {
			return result, ErrMediaMissing
		}
		if e = s.Store.TouchVideo(ctx, c, video.ID); e != nil {
			return result, e
		}
		result.Range = &domain.Range{StartMS: w.StartMS, EndMS: w.EndMS}
	}
	var total int64
	for i := 0; i < w.Count; i++ {
		requested := w.StartMS + (w.EndMS-w.StartMS)*int64(2*i+1)/int64(2*w.Count)
		if w.Crop != nil {
			requested = *parent.RequestedTimeMS
		}
		cacheKey := "frame:" + *c.Run.SnapshotID + ":" + store.Hash(map[string]any{"time": requested, "mode": w.Mode, "crop": w.Crop, "version": 1})
		var cached struct {
			ID string `json:"id"`
		}
		if s.Resources.Cached(ctx, cacheKey, &cached) {
			a, e := s.Store.Asset(ctx, cached.ID)
			if e == nil && a.State == "ready" {
				path, _ := s.Path(a.StorageKey)
				if _, e = os.Stat(path); e == nil {
					if e = s.Store.Pin(ctx, c, a.ID); e == nil {
						result.FrameIDs = append(result.FrameIDs, a.ID)
						total += a.Bytes
						continue
					}
				}
			}
		}
		fctx, release, e := s.Resources.Acquire(ctx, c, "ffmpeg")
		if e != nil {
			return result, e
		}
		sub, cancel := context.WithTimeout(fctx, 45*time.Second)
		file := filepath.Join(work, domain.ID()+".jpg")
		args := []string{"-nostdin", "-v", "info", "-threads", "2"}
		if w.Crop == nil {
			args = append(args, "-ss", fmt.Sprintf("%.6f", float64(requested)/1000), "-copyts")
		}
		args = append(args, "-i", input)
		long := 1280
		if w.Mode == "detail" {
			long = 1920
		}
		filter := fmt.Sprintf("showinfo,scale=w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease", long, long)
		if w.Crop != nil {
			filter = fmt.Sprintf("crop=%d:%d:%d:%d,", w.Crop.Width, w.Crop.Height, w.Crop.X, w.Crop.Y) + filter
		}
		args = append(args, "-vf", filter, "-filter_threads", "1", "-frames:v", "1", "-threads", "2", "-q:v", "3", "-y", file)
		log := &boundedBuffer{max: 65536}
		_, e = commandLogged(sub, "ffmpeg", args, work, 3<<30, 0, log)
		cancel()
		release()
		if e != nil {
			return result, e
		}
		actual := requested
		if w.Crop != nil {
			actual = *parent.TimeMS
		} else {
			match := ptsTime.FindSubmatch(log.Bytes())
			if len(match) != 2 {
				return result, errors.New("decoder did not report actual frame time")
			}
			sec, e := strconv.ParseFloat(string(match[1]), 64)
			if e != nil {
				return result, e
			}
			actual = int64(sec*1000 + 0.5)
			if actual < 0 || actual >= snap.DurationMS {
				return result, errors.New("decoded frame outside source")
			}
		}
		f, e := os.Open(file)
		if e != nil {
			return result, e
		}
		dim, _, e := image.DecodeConfig(f)
		f.Close()
		if e != nil {
			return result, e
		}
		a, e := s.own(ctx, c, file, "frame", "image/jpeg", c.Run.SnapshotID, 24*time.Hour)
		if e != nil {
			return result, e
		}
		if a.Bytes > 2<<20 || total+a.Bytes > 8<<20 {
			return result, domain.Err(422, "RESOURCE_LIMIT_EXCEEDED", "图片载荷超限，请缩小窗口或裁剪。")
		}
		a.TimeMS = &actual
		a.RequestedTimeMS = &requested
		a.Width = dim.Width
		a.Height = dim.Height
		a.Crop = w.Crop
		a.Caption = "视频画面"
		if e = s.Store.AddAsset(ctx, c, a); e != nil {
			return result, e
		}
		s.Resources.Cache(ctx, cacheKey, map[string]string{"id": a.ID})
		total += a.Bytes
		result.FrameIDs = append(result.FrameIDs, a.ID)
	}
	if e = s.Store.FinishOperation(ctx, c, key, result, "", nil); e != nil {
		return result, e
	}
	return result, nil
}
