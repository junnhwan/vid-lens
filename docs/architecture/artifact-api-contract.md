# Artifact API — first delivery

Contract version: 1 (2026-09-27). This file is the frontend/backend integration authority; the routes and fields below were re-checked against `cmd/server/router.go` and the handlers. All paths have `/api/v1` prefix and require the existing Bearer JWT. IDs for artifacts, versions, runs, manifests and evidence are opaque strings; video IDs remain positive integers. Existing chat APIs and disconnect semantics are unchanged.

R1 Agent revision is defined by [`artifact-editing-contract.md`](artifact-editing-contract.md). That extension adds edit runs/operations, `origin=agent|undo`, and `edit_operation_id`; where its Agent-edit rules differ from the first-delivery human-save wording below, the R1 extension is authoritative. Manual save, generation, and answer-import behavior in this document remains unchanged.

## Scope and representation

### Product completion extension (2026-09-27)

`GET /artifacts` list items and `GET /artifacts/:id` detail now include required `latest_run: Run|null`. The server selects the newest owner-scoped generation attempt by run `created_at DESC, id DESC`. Task pagination and the time of an older run's late status update do not affect this selection. Existing readable `version` remains available after a later failed, cancelled, or budget-limited attempt. An artifact with no attempt has `latest_run:null`.

`Run` now includes required positive integer `source_task_id`, the source video task ID. `Run.id` is the run ID; `Run.artifact_id` is the artifact ID. The namespaced task `id` remains a task-row identity. For artifact task rows, `resource_id` remains the run ID; for media task rows it remains the video task ID string. Clients navigate using the explicit `run.artifact_id`, `run.source_task_id`, and `run.result`, rather than matching `resource_id` to an artifact ID. Generation submissions still validate owner access and source readiness on the server.

First delivery supports `kind: "study"`, `scope: "video"`, exactly one `source_ids` video. It reads canonical transcript/visual observations without requiring a vector index. Notes and the mind map share the same ordered block tree; a map is a view, not another model call. Other kinds/scopes return `unsupported_recipe`.

Success envelope: `{ "code": 200, "message": "success", "data": ... }` (202 for accepted runs). Error envelope: `{ "code": 409, "message": "...", "data": { "error_code": "version_conflict" } }`. `code` is the HTTP status, not a string error enum. Internal/provider error text is not exposed. Owner mismatches return 404. Demo accounts cannot mutate.

```json
{
  "schema_version": 1,
  "kind": "study",
  "title": "课程学习笔记",
  "blocks": [
    {"block_id":"chapter-1","parent_id":null,"type":"section","title":"核心概念","content":"本章概览","claim_origin":"source","evidence_refs":[{"evidence_id":"opaque-evidence-id","relation":"supports"}]},
    {"block_id":"concept-1","parent_id":"chapter-1","type":"concept","title":"概念","content":"说明","claim_origin":"source","evidence_refs":[{"evidence_id":"opaque-evidence-id","relation":"supports"}]}
  ],
  "warnings": []
}
```

This object is `body`. Block types: `section|concept|example|note`; claim origins: `source|synthesis|user`; relations: `supports|context|contradicts`. Parent must precede child; IDs unique; at most 200 blocks, depth 8, title 200 characters, block content 8,000 characters, total body 512 KiB. Source claims require references. References must belong to the server-frozen manifest; the client/model cannot create evidence identities. Edited/new blocks are stamped `user` by the server and receive `human_edited_unverified`; keeping a citation does not revalidate its support. Render text/Markdown safely; never execute model HTML/JS or graph configuration.

Artifact metadata: `{id,kind,title,head_version,current_version_id,created_at,updated_at}`. `head_version` is an integer CAS counter (0 before first generation); `current_version_id` is nullable. Detail adds required `version: Version|null`. Version: `{id,artifact_id,version,base_version,origin,run_id,manifest_id,body,quality,created_at,source_status,was_candidate,adopted_from_version_id}`. Every listed field is required. `run_id` and `adopted_from_version_id` are `string|null`; all other Version fields are non-null. `origin=generated|user`, `quality=needs_review` for generated/edited drafts. Versions are immutable; candidate versions need not be current. `version` is a monotonically increasing revision number; the head counter points to the adopted revision number. The database, not only the service, enforces monotonicity: the unique index `idx_artifact_revision` on `(artifact_id, version)` rejects a duplicated or rewound revision number, the unique `edit_operation_id` column allows at most one version per Agent edit operation, and the unique index `idx_artifact_run_output` on `(run_id, output_role)` allows at most one version per run and output role (this pipeline always stores `output_role=study`). A new number is assigned as `MAX(version)+1` for that artifact inside the publishing transaction, so a concurrent writer cannot reuse it.

`VersionSummary` has exactly `{id,artifact_id,version,base_version,origin,run_id,manifest_id,quality,created_at,was_candidate,adopted_from_version_id}` with the same required/nullable rules. It omits `body` and `source_status`. Fetch a specific Version to check its live source status. `source_status=current|outdated` is calculated at read time; `outdated` preserves frozen evidence text and never silently substitutes new observations. Disable timestamp seeking for outdated versions and suggest regeneration. Source deletion returns 410 rather than a third readable status. `was_candidate` is an immutable boolean describing creation; `adopted_from_version_id` links a new adoption revision to its original candidate. Run `result.is_candidate` means an originally-unadopted generated revision still awaits adoption; later human edits do not turn a previously current generated revision into a candidate.

## Routes

| Method / route | Request | Response data |
| --- | --- | --- |
| POST `/artifact-runs` | GenerationRequest, required `Idempotency-Key` | Run (202) |
| GET `/artifact-runs/:id` | — | Run |
| POST `/artifact-runs/:id/cancel` | `{}` | current Run (200), including any already-won terminal state |
| POST `/artifact-runs/:id/retry` | `{}`, required new `Idempotency-Key` | new Run (202), `parent_run_id` references old terminal run |
| POST `/artifact-runs/:id/resume` | `{}` | current Run (202); only active runs, queues recovery without bypassing a live lease |
| GET `/artifact-runs/:id/events?after_seq=0` | optional `Last-Event-ID` numeric cursor; query wins | SSE, described below |
| GET `/artifacts` | optional `source_id`, `page` (1), `page_size` (20, max 100) | `{list: Artifact[],total,page,page_size}` |
| POST `/artifacts` | `{source_ids:[42],body:Body}` | Artifact detail (200); user-authored initial revision, source refs obtained from the source catalog below |
| GET `/artifacts/:id` | — | Artifact detail; source-deleted current body returns 410 |
| GET `/artifacts/:id/versions` | — | `{list: VersionSummary[]}`; summaries omit body |
| GET `/artifacts/:id/versions/:version_id` | — | Version; 410 when its source is deleted |
| PATCH `/artifacts/:id` | `{expected_head_version:1,body:Body}` | Artifact detail; writes a new immutable user revision |
| POST `/artifacts/:id/adopt` | `{expected_head_version:1,version_id:"..."}` | Artifact detail; adopts a candidate by creating a new user revision |
| GET `/artifacts/:id/blocks/:block_id/context` | required `version_id` query | `{artifact_id,version_id,task_id,block}`; one stored block of that version. Unknown version or block is 404 `not_found` |
| POST `/artifacts/:id/answer-preview` | `{message_id,after_block_id,expected_head_version}` | `{message_id,content,after_block_id,mapped,unmapped,version_id}`; resolves a saved chat answer's citations against that version's frozen manifest and writes nothing |
| POST `/artifacts/:id/answer-import` | same plus `personal_without_sources`, required `Idempotency-Key` | Artifact detail; appends one `note` block stamped `user` after `after_block_id` as a new user revision |
| GET `/sources/video/:id` | — | `{manifest_id,source_id,title,evidence:Evidence[]}`; freezes/reuses canonical snapshot |
| GET `/sources/:manifest_id/evidence/:evidence_id` | — | Evidence; rechecks source access; 410 after deletion |
| GET `/tasks` | optional `page`, `page_size` | `{list: Task[],total,page,page_size}`; media and artifact generation read projection |

GenerationRequest:

```json
{"kind":"study","scope":"video","source_ids":[42],"goal":"整理为中文学习笔记","artifact_id":null,"base_version":0}
```

Omit `artifact_id`/`base_version` to create a new artifact. For regeneration supply both; the base must be current at submission. Goal max 2,000 characters. Uses the user's default model profile and effective budget at submission. Profile identity/configuration are frozen without keys; credentials are resolved by profile ID at execution. Deleted profiles, unavailable credentials or changed model/endpoint/context configuration produce `profile_changed`, never silently use another default profile. Key rotation on the same profile is allowed. At most 2 MiB canonical source text and 1,000 observations; larger inputs fail `source_limit_exceeded`. Unfinished transcript chunks fail `source_not_ready`; they are not silently omitted. Sources changing before final commit fail `source_changed`; historical snapshots never silently point to new evidence.

Run:

```json
{"id":"run-id","artifact_id":"artifact-id","parent_run_id":null,"status":"pending","stage":"queued","cancel_requested":false,"can_cancel":true,"can_retry":false,"can_resume":true,"result":null,"error_code":null,"created_at":"2026-09-27T00:00:00Z","started_at":null,"finished_at":null,"last_seq":1,"usage":{"llm_calls":0,"prompt_tokens":0,"completion_tokens":0,"token_source":"unknown"}}
```

Statuses: `pending → running → completed|failed|cancelled|budget_exhausted`. Stages: `queued|collecting|generating|validating|completed|failed|cancelled|budget_exhausted`. Cancellation is an intent until acknowledged; only one terminal result wins. `result` is null or `{artifact_id,version_id,quality:"needs_review",is_candidate:false}`. Regeneration after concurrent editing saves a candidate (`is_candidate:true`), never overwrites the head. Only explicit adoption changes a candidate into current content. `started_at` is fixed on first claim; queue deadline and execution budget are independent. Usage is cumulative across attempts and marked `unknown|estimated|actual|mixed`; unknown costs are never displayed as zero cost. Queue deadline is 24 hours. Run call/time/input/output limits freeze the existing profile's effective Agent budget; no new UI configuration is required.

Task: `{id,type,resource_id,title,status,stage,can_cancel,can_retry,can_resume,created_at,updated_at,run:Run|null}`. `type=artifact_generation|video_processing`; namespaced `id` prevents collisions; media `status` is a string of its existing numeric status, actions are false in this projection. Use existing media routes for media actions. Chat history remains under chat routes.

Evidence: `{id,manifest_id,source_id,source_title,source_identity,modality,content,content_hash,start_ms,end_ms,time_range_status}`. `source_identity` is the canonical timeline atom identity, not a retrieval chunk. Precision is `precise|coarse|unknown`; unknown times are null, never fabricated 00:00. Playback uses existing `GET /media/task/:source_id/playback`; seek to `start_ms/1000` only when not null, label coarse times approximate. Evidence content is snapshot data; signed playback URLs are not persisted. Deleted sources block dependent version/evidence reads and cancel outstanding runs; snapshot/checkpoint content is purged while artifact metadata and independent user notes remain.

## Summary revision and term-rule routes

Summary text is a second durable Agent subsystem, not an artifact: recipe `summary-edit-v2`, run subject `summary_edit_request`, its own `summary_edit_dispatches` outbox and `vidlens.summary.edit.v1` queue, and CAS on the summary's own `expected_revision` instead of an artifact `expected_head_version`. `Idempotency-Key` is required only on submission. There is no SSE route here; clients read the operation projection, whose fields are `{id,run_id,task_id,instruction,status,mode,base_version,rule_version,rule_digest,edits,result_revision_id,undo_revision_id,error_code}` with `status=running|proposed|committed|failed` and at most 20 `edits`. Submission rejects an empty summary (`source_not_ready`), a summary longer than 48,000 characters (`source_limit_exceeded`), a missing usable default profile (`profile_required`), and a stale `expected_revision` (`version_conflict`).

| Method / route | Request | Response data |
| --- | --- | --- |
| GET `/media/task/:id/summary` | — | `{task_id,content,revision,revision_id,base_generated_hash,current_generated_hash,source_status,has_generated,has_revision}`; `source_status=current|generated_missing|needs_merge` |
| GET `/media/task/:id/summary/export` | — | `text/markdown` attachment named `video-<id>-summary-v<revision>.md`; 422 `source_not_ready` when there is no content |
| POST `/media/task/:id/summary/edit-runs` | `{instruction,expected_revision,mode:preview\|apply}`, required `Idempotency-Key` | operation view (202); `instruction` 1–2,000 characters |
| GET `/media/task/:id/summary/operations/latest` | — | operation view, or `null` when the task has no attempt |
| GET `/media/task/:id/summary/operations/:operation_id` | — | operation view |
| POST `/media/task/:id/summary/operations/:operation_id/apply` | `{expected_revision}` | operation view with `status=committed`; an already-committed operation replays its result, while a `proposed` operation whose `base_version` differs from `expected_revision` returns 409 `version_conflict` |
| POST `/media/task/:id/summary/operations/:operation_id/undo` | `{expected_revision}` | operation view with `undo_revision_id` set; a stale head is 409 `version_conflict` and an operation with no committed result is 409 `undo_conflict` |
| POST `/media/task/:id/summary/resolve-base` | `{expected_revision,choice:keep_revision\|use_generated}` | `{task_id,content,revision,source_status}`; creates the next revision only while `source_status=needs_merge`, otherwise 422 `nothing_to_change` |
| GET `/media/task/:id/term-rules` | — | `{version,digest,rules}`; `evidence_supported` rules are downgraded to `evidence_status=pending_review` when the source hash changed |
| PUT `/media/task/:id/term-rules` | `{expected_version,from,to,context,exclusions,transcript_evidence_id?,visual_evidence_id?,linked_operation_id?}` | the new `{version,digest,rules}` set; `from`/`to` ≤80, `context` ≤240, at most 8 exclusions of ≤120 characters, `from != to` |
| POST `/media/task/:id/term-rules/:rule_id/disable` | `{expected_version}` | the new `{version,digest,rules}` set |

Term-rule writes CAS on `expected_version` and return 409 `version_conflict`; the frozen `rule_version`/`rule_digest` recorded with each edit run is what that run's planning saw. These routes reuse the artifact error codes and the same envelopes as above.

## Idempotency, recovery, and conflicts

`Idempotency-Key`: 1–128 visible ASCII characters (no spaces); scoped to owner and generation operation. Same key and canonical request returns the original run (202, including terminal runs). Different payload returns 409 `idempotency_conflict`; retries of network failures must reuse the key. Terminal user retry creates a new run/request with a new budget and freezes the current head at retry submission; active resume keeps the same run and accumulated budget. Durable outbox plus RabbitMQ use at-least-once delivery; PostgreSQL run leases/epochs fence old workers. A periodic scan recovers pending/expired runs even after broker Ack, and enforces queue/execution deadlines even when the broker is unavailable. No exactly-once model-call claim. An interrupted uncheckpointed call is recorded `ambiguous`, reserves conservative estimated tokens, and may be retried only within the fixed call budget. Each segment call allows at most two provider attempts across recovery; one independent format-repair call is permitted within the same total budget. Transient provider errors use bounded retries and respect Retry-After.

Edits/adoption require `expected_head_version`; mismatch is HTTP 409 `version_conflict`, with no write. Fetch head and reconcile explicitly; never silently retry a stale write. Generated output uniqueness is `(run_id, output_role)`; revision insertion, source dependency, run completion and completion event commit atomically. Cancel races return the committed winner.

## Events

SSE subscribes only; disconnect never cancels work. Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`. Each durable event:

```text
id: 2
event: run.updated
data: {"schema_version":1,"run_id":"run-id","seq":2,"type":"run.updated","created_at":"2026-09-27T00:00:00Z","data":{"status":"running","stage":"generating"}}

```

Types: `run.created|run.updated|run.cancel_requested|run.completed|run.failed|run.cancelled`. Payload contains safe state/phase/result references only, no prompt, reasoning, credentials or evidence body. Sequence is per run, increasing and durable. Deduplicate `seq`, reconnect with `after_seq` and refresh the run on terminal events. Heartbeats are SSE comments, not events. EOF alone is not success; reconnect/query. First delivery retains events for the run lifetime (no expiration/reset yet). Invalid/negative or future cursors return 400 `invalid_cursor` before streaming; HTTP authentication/error envelope applies before SSE starts.

## Stable errors

| HTTP | error_code | Meaning |
| --- | --- | --- |
| 400 | `invalid_request`, `invalid_cursor`, `invalid_evidence`, `unsupported_recipe` | invalid input/schema/reference/scope |
| 401 / 403 | existing JWT/demo policy | authentication or demo mutation denied |
| 404 | `not_found` | absent resource or not owned |
| 409 | `idempotency_conflict`, `version_conflict`, `run_not_terminal`, `run_terminal`, `source_changed` | reconcile state; do not blind retry |
| 410 | `source_deleted` | dependent body/evidence no longer readable |
| 422 | `source_not_ready`, `source_limit_exceeded`, `profile_required` | source/configuration needs user action |
| 500 | `internal_error` | safe error; retry reads or same-key submission |

Run terminal `error_code` additionally includes `profile_changed`, `unsupported_checkpoint`, `invalid_model_output`, `provider_error`, `provider_truncated`, `provider_refused`, `budget_exhausted`, `queue_expired`, `source_not_ready`, `source_changed`, `source_deleted`. Validation establishes schema and permitted references, not factual accuracy; generated drafts remain `needs_review`.

## Runtime and verification

The server starts a separate artifact worker lifecycle (one in-flight generation per process) on durable queue `vidlens.artifact.generate.v1`. `go run ./cmd/artifact-worker -config config.yaml` is the standalone worker entry point; it expects the API server's schema migration to have run (or use explicit `-migrate` when starting without the server). Both modes share leases and outbox, so multiple processes do not acquire the same live run. On shutdown the provider context is cancelled and worker goroutines join; the run remains recoverable. The worker uses the existing encrypted profile, quota admission, and PostgreSQL journal. No Eino migration or new orchestrator dependency is required for this deterministic recipe.

The first recipe splits all canonical observations into bounded segments and generates each with strict JSON/reference validation, then merges their ordered trees. Completed segment bodies are durable checkpoints. `covered_segments:N/N` in warnings records complete segment traversal; `coverage_is_observations_not_all_video_frames` prevents interpreting available OCR/ASR coverage as exhaustive video perception. Budget/size overflow fails explicitly instead of silently dropping unprocessed segments. The worker uses the existing provider streaming client and buffers at most 512 KiB of answer text per call before validation; partial, interrupted or truncated streams never publish a version. Provider usage is collected from the stream. This avoids depending on a gateway holding a long non-streaming response open; clients without a streaming interface retain the ordinary Chat path. The provider compatibility path uses JSON text with local strict validation; provider-enforced JSON Schema support is not assumed. The integration tests use a local HTTP model fixture and real isolated PostgreSQL/RabbitMQ; they do not establish real-model semantic quality.

Private segment checkpoints and dispatch rows are purged seven days after a terminal run; source deletion clears checkpoint/evidence content immediately in its transaction. Immutable product versions remain source-gated. Run events remain for the run lifetime. Per-call `agent_tool_calls.id` is the provider-call accounting identity: actual token reports can adjust that registered call exactly once after cancellation/lease loss without writing content or changing status. Run token totals distinguish actual/estimated/mixed. There is no monetary cost limit in this pipeline and no price table anywhere in the runtime: artifact generation and edit runs freeze call, token, time, step and context ceilings from the profile's effective Agent budget and leave `max_cost_micros` at 0, meaning "money unknown" rather than a fabricated budget, and provider usage never yields a cost figure, so cost usage is recorded as unknown rather than as zero money. The `max_cost_micros` column and its ceiling check exist, but `cost_micros_used` has no writer anywhere in the implementation (`internal/repository/agent_execution.go:723` reads it only), so the check cannot fire even for the Chat/Agent path, which freezes the field to 1000000 micros (`internal/service/agent_execution.go:114`); only the non-monetary limits bind. The existing global daily quota governor still uses its legacy estimated settlement, so run token accounting must not be described as a completed migration of all quota/cost accounting.

## Source compatibility evidence

Existing canonical projection: `internal/service/video_timeline.go:15` (`TimelineAtom`) and `:111` (`BuildVideoTimeline`); owner-checked timeline and playback: `internal/service/media_tasks.go:447` (`GetVideoTimeline`) and `:482` with `:491` (`GetPlaybackURL` and its owner check); chat journal/session coupling: `internal/service/agent_execution_journal.go:66` with `:87` (`EnsureRun` requires a session and stores its ID); HTTP cancellation behavior: `internal/handler/conversation_stream.go:15` with `:65` (the response context is cancelled by the request and the loop returns on `ctx.Done`). These were re-checked against the current source; new types and transactional behavior live under `internal/artifact`, `internal/model/artifact.go`, and `internal/repository/artifact_*`.
