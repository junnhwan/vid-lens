package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vidlens/internal/domain"
)

func TestQuestionFrameRefinementUsesNearbyPixelsAndReplaysBoundedBudget(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("native ffmpeg unavailable")
	}
	s, _ := deliveryFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("nearby extraction made an extra model call") })
	t.Cleanup(func() {
		ctx := context.Background()
		keys, err := s.engine.Resources.Redis.Keys(ctx, "vidlens:cache:*").Result()
		if err != nil {
			t.Error(err)
			return
		}
		for _, key := range keys {
			value, err := s.engine.Resources.Redis.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			var cached struct {
				Key string `json:"key"`
			}
			if json.Unmarshal([]byte(value), &cached) == nil && strings.HasPrefix(cached.Key, "frame:"+s.snapshot.ID+":") {
				if err = s.engine.Resources.Redis.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, value).Err(); err != nil {
					t.Error(err)
				}
			}
		}
	})
	ctx := context.Background()
	catalog, err := s.citationCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent := catalog["f1"].FrameID
	s.cp.Inspections = map[string][]string{"seed": {parent}}
	filename := domain.ID() + ".mp4"
	path := filepath.Join(s.engine.Config.DataDir, filename)
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10:duration=24", "-c:v", "mpeg4", "-threads", "1", "-y", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	video := domain.Asset{ID: domain.ID(), SnapshotID: &s.snapshot.ID, SourceID: s.claim.Run.SourceID, RunID: s.claim.Run.ID, Kind: "video", StorageKey: filename, SHA256: sha, MIME: "video/mp4", Bytes: int64(len(data))}
	if err = s.engine.Store.AddAsset(ctx, s.claim, video); err != nil {
		t.Fatal(err)
	}
	s.snapshot.DurationMS, s.snapshot.VideoHash = 24000, sha
	if _, err = s.engine.Store.Pool.Exec(ctx, "UPDATE source_snapshots SET duration_ms=$2,video_hash=$3 WHERE id=$1", s.snapshot.ID, s.snapshot.DurationMS, sha); err != nil {
		t.Fatal(err)
	}
	if _, err = s.makeTools(); err != nil {
		t.Fatal(err)
	}
	in := refineFrameInput{QuestionID: "diagram", Question: "图中有什么？", FrameIDs: []string{parent}, Reason: "合成测试：原采样点不呈现所需细节，检查附近时刻。"}
	if _, err = planCall(s, "nearby-one", "refine_video_frames", in); err != nil {
		t.Fatal(err)
	}
	first := append([]string{}, s.cp.Inspections["nearby-one"]...)
	if len(first) != 2 || len(s.cp.FrameRefinements) != 1 {
		t.Fatal("missing batched refinement", first, s.cp.FrameRefinements)
	}
	for _, id := range first {
		a, bytes, err := s.engine.Media.ReadImage(ctx, s.claim, id)
		if err != nil || len(bytes) == 0 || a.TimeMS == nil || *a.TimeMS < 0 || *a.TimeMS > 5000 {
			t.Fatal("not actual nearby pixels", a, err)
		}
	}
	if _, err = planCall(s, "nearby-one", "refine_video_frames", in); err != nil {
		t.Fatal("replay failed", err)
	}
	if strings.Join(first, ",") != strings.Join(s.cp.Inspections["nearby-one"], ",") || len(s.cp.FrameRefinements) != 1 {
		t.Fatal("replay created new evidence or credit")
	}
	if _, err = planCall(s, "nearby-two", "refine_video_frames", in); err != nil {
		t.Fatal(err)
	}
	if len(s.cp.Inspections["nearby-two"]) != 2 || len(s.cp.FrameRefinements) != 2 {
		t.Fatal("wider second batch missing")
	}
	feedback, err := planCall(s, "nearby-three", "refine_video_frames", in)
	if err != nil || !strings.Contains(string(domain.JSON(feedback)), "额度已用完") || len(s.cp.Inspections["nearby-three"]) != 0 {
		t.Fatal("unbounded extraction", feedback, err)
	}
	var calls int
	if err = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM model_submissions WHERE run_id=$1", s.claim.Run.ID).Scan(&calls); err != nil || calls != 0 {
		t.Fatal("one call per sampled frame", calls, err)
	}
}
