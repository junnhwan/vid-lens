# Summary source integration audit

Date: 2026-10-10. Pre-implementation source inspection at baseline `881a13c`; the findings below describe that baseline, not the current implementation. Platform and model calls were not executed during this initial audit. For implemented changes and later live validation, see [delivery progress](2026-10-10-summary-delivery-progress.md).

## Durable handoff

- `internal/repository/repository.go`: `Transaction` rebuilds repositories with the transaction DB. Compose publication, job completion and next dispatch inside one outer transaction.
- `internal/repository/task_lease_terminal.go`: `CompleteTaskProcessing` fences parent/job completion by processing token, expiry and version.
- `internal/repository/task_dispatch_initial.go`: `PrepareInitialTaskDispatch` writes job, retry budget and recoverable dispatch lease. Dispatch network calls follow commit.
- `internal/repository/task_lease_ownership.go`: `RunWithTaskProcessingLease` fences publication; remote operations remain at least once.
- `internal/repository/transcription_projection.go`: `SaveTranscriptionAndInvalidateIndex` keeps compatible text/index invalidation together.
- `internal/mq/consumer_download.go`: current completion publishes an uploaded asset but creates no subsequent source job.
- `internal/mq/transcription_workflow.go`: `publish` writes text before the visual/index tail. If new publication ends ASR lease and dispatches summary, `handleTranscribe` must branch before `waitForVisualAfterASR`, `indexAfterTranscription` and `generateTitle`, which require that lease.
- `internal/service/media_tasks.go`: `RequestTranscribe` rejects running/queued parents; fallback must prepare ASR dispatch in the repository transaction instead.

## Frozen generation

- `internal/repository/summary_job.go`: independent summary lease/input snapshot exists; current force path deletes the previous readable summary and reads MD5 caches.
- `internal/mq/consumer_analyze.go`: `summarizeTask` reads live preference/default profile/context/model; source generations need frozen settings.
- `internal/mq/consumer_transcribe.go`: `strategyForTask` resolves current default profile.
- `internal/repository/summary_part.go`: checkpoint identity is task/level/index; a new generation must reset or fingerprint checkpoints by source/options/generation.

## Source readers

`internal/service/task_transcript_source.go` supplies timeline, retrieval, artifact freeze, chat and question suggestions. Its MD5 fallback and ASR chunk return type require source-aware adaptation. Callers include `media_tasks.go:GetVideoTimeline`, `rag_index_build.go:loadTaskIndexChunks`, `artifact_workspace.go:artifactSource`, `chat_prepare.go`, and `video_questions.go`.

`internal/service/video_timeline.go:BuildVideoTimeline` labels transcript atoms ASR; `transcript_spans.go:transcriptObservations` assigns ASR identities/window semantics. Subtitle cues require their own observations with source/cue/timing metadata; synthetic ASR rows would misrepresent provenance.

## Legacy cache boundaries

Repository queries in `transcription.go`, `summary.go`, and `task.go` must exclude source/document/generation-scoped rows from shared MD5 fallback. Tasks with active source or frozen processing intent cannot consume shared fallback. Direct task-owned results remain visible. Structured/private summaries must be deleted with their task, never rehomed. Legacy summary rehome may choose only legacy-eligible successor tasks.

Remaining caller guards: `internal/service/media_tasks.go` (analysis, transcription, details), `task_transcript_source.go`, `content_dedup.go`, `media_file_upload.go:createTaskFromAsset`, `chat_prepare.go`, `ai_reliability_degraded.go`, `video_questions.go`, `internal/repository/summary_job.go`, and `summary_revision.go`. File asset reuse stays separate from result/source reuse.

## Registration and cleanup

New source jobs need explicit model/stage constants, producer payload/routing, consumer branch/start and `retry.go:retryDispatchState`/`enqueueRetry` registration. Parent-owned source jobs reuse generic lease/retry discovery. A separate queue additionally requires config/defaults/validation, `cmd/server/main.go` queue declaration and `wiring.go` consumer start.

`internal/service/task_cleanup.go:RequestDelete` guards active parent and independent summary jobs. `deleteTaskOwnedRows` and object cleanup need source/cue/raw subtitle deletion. Source publication and idempotent replays must check task survival/ownership. New dispatch/source refresh and summary publication need stale-token/source/generation regression coverage.

## API seams

`internal/handler/media.go:UploadByURL` and `MergeChunks` currently accept no processing options or request idempotency key. `internal/service/media_chunk_upload.go:MergeChunks` has several asset-cache exits; all new option-aware exits must converge on intent-aware task creation. `TaskRepository.ListByUserID` uses explicit columns and needs source/intent projection for accurate cache/readiness decisions.
