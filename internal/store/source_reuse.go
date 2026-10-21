package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"vidlens/internal/config"
)

func sourcePreparationKey(source string, p config.Profile, pipeline string) string {
	return source + ":prepare-v2:" + Hash(map[string]any{"asr_endpoint": p.ASRBaseURL, "asr_model": p.ASRModel, "asr_key_ref": p.ASRKeyRef, "max_video_ms": p.MaxVideoMS, "max_download_bytes": p.MaxDownloadBytes, "pipeline": pipeline})
}

type retainedSource struct {
	RunID, SnapshotID string
	VideoReady        bool
}

// Original text belongs to an immutable source snapshot, not an interpretation
// model or its admission limits. Reuse completed anonymous ingestion from the
// same pipeline when the actual source fits. A retry pins its original snapshot.
func findRetainedSource(ctx context.Context, tx pgx.Tx, source, pipeline, pinned string, p config.Profile) (*retainedSource, error) {
	rows, err := tx.Query(ctx, `SELECT r.id,r.work_key,s.id,c.profile
 FROM job_runs r JOIN source_snapshots s ON s.id=r.result_id
 JOIN video_sources v ON v.id=s.source_id
 JOIN runtime_config_revisions c ON c.id=r.config_id
 WHERE r.kind='source.prepare' AND r.status='succeeded' AND r.source_id=$1 AND s.source_id=$1
 AND s.duration_ms>0 AND s.duration_ms<=$2 AND s.text_hash<>''
 AND ($4='' OR s.id=$4)
 AND EXISTS(SELECT 1 FROM transcript_segments t WHERE t.snapshot_id=s.id AND btrim(t.text)<>'')
 AND EXISTS(SELECT 1 FROM media_assets a WHERE a.snapshot_id=s.id AND a.kind='video' AND a.sha256=s.video_hash AND a.bytes<=$3)
 AND ($4<>'' OR (s.source->'text_provenance'->>'coverage'='complete'
   AND s.metadata->>'access_policy'='anonymous'
   AND (v.platform<>'bilibili' OR s.metadata->>'extractor'='bilibili_public_api_v1')))
 ORDER BY s.created_at DESC,r.updated_at DESC`, source, p.MaxVideoMS, p.MaxDownloadBytes, pinned)
	if err != nil {
		return nil, err
	}
	var selected *retainedSource
	for rows.Next() {
		var candidate retainedSource
		var key string
		var raw []byte
		if err = rows.Scan(&candidate.RunID, &key, &candidate.SnapshotID, &raw); err != nil {
			break
		}
		var original config.Profile
		if err = json.Unmarshal(raw, &original); err != nil {
			break
		}
		// Reconstruct with the original ingestion parameters to retain the
		// frozen extractor/pipeline boundary without comparing model limits.
		if pinned != "" || key == sourcePreparationKey(source, original, pipeline) {
			selected = &candidate
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || selected == nil {
		return nil, err
	}
	var asset string
	err = tx.QueryRow(ctx, `SELECT a.id FROM media_assets a JOIN source_snapshots s ON s.id=a.snapshot_id
 WHERE a.snapshot_id=$1 AND a.kind='video' AND a.sha256=s.video_hash AND a.bytes<=$2
 AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>now()
   OR EXISTS(SELECT 1 FROM asset_refs ar WHERE ar.asset_id=a.id AND (ar.expires_at IS NULL OR ar.expires_at>now())))
 ORDER BY a.created_at DESC LIMIT 1 FOR SHARE OF a`, selected.SnapshotID, p.MaxDownloadBytes).Scan(&asset)
	if errors.Is(err, pgx.ErrNoRows) {
		return selected, nil
	}
	if err != nil {
		return nil, err
	}
	selected.VideoReady = true
	return selected, nil
}
