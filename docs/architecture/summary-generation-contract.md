# Summary generation reads and recovery

A summary generation belongs to one authenticated user, video task and frozen generation ID. It uses the task's accepted processing intent, captured source ID/digest, profile fingerprint, recipe and budget. Import replay returns the same task and its immutable initial generation ID, even after an explicit regeneration changes the task’s current workflow; a changed request with the same idempotency key returns 409. The source generation never becomes a chat session.

## Routes

These routes use the existing JWT group and standard `{code, message, data}` response envelope.

- `POST /api/v1/media/task/:id/summary?force=1`: explicit generation or regeneration; the existing `/api/v1/media/analyze/:id` route is an equivalent compatibility entry. Active or retry-waiting work returns 409 instead of duplicate dispatch.
- `POST /api/v1/media/task/:id/summary/visual-retry`: explicitly authorized visual-only attempt for an existing typed text result; see below.
- `GET /api/v1/media/task/:id/summary/generation`: latest accepted generation snapshot.
- `GET /api/v1/media/task/:id/summary/generation/:generation_id/events?after_seq=0&limit=100`: ordered durable event recovery, including an older generation still owned by this task.
- `GET /api/v1/media/task/:id/summary`: existing effective summary, canonical `document`, typed content digest/hash kind and existing version reference. The generation reader does not introduce another document version.

Inaccessible, foreign or deleted tasks, and a generation belonging to another task, return 404. Invalid IDs/cursors, duplicate cursor parameters, and limits outside 1–100 return 400. A cursor beyond the run's high-watermark returns 409 `event_cursor_ahead`.

## Snapshot

```json
{
  "task_id": 123,
  "generation_id": "accepted-generation-uuid",
  "result_generation_id": "accepted-generation-uuid",
  "legacy": false,
  "mindmap_enabled": true,
  "requested_mode": "auto",
  "resolved_mode": "text",
  "text_state": "ready",
  "visual_state": "running",
  "result_state": "ready",
  "stage": "visual_enrichment",
  "status": "running",
  "source_status": "current",
  "source": {
    "id": "source-uuid",
    "kind": "subtitle",
    "digest": "sha256",
    "language": "zh",
    "quality": "usable"
  },
  "generated_version": 1,
  "content_digest": "sha256",
  "content_hash_kind": "summary-json-v2",
  "event_high_watermark": 5,
  "activities": [{
    "id": "summary-complete",
    "kind": "plan",
    "state": "done",
    "title": "核对来源并整理主要结论",
    "attempt": 1,
    "started_at": "2026-10-10T03:00:00Z",
    "finished_at": "2026-10-10T03:00:02Z",
    "duration_ms": 2000
  }]
}
```

`text_state` describes the selected generation. `result_state` describes whether the existing effective summary can be read. While a new generation queues, its text can remain pending while the previous result is ready; `result_generation_id` identifies that previous result. Generated version and hashes are the existing effective-summary metadata. `source_status` comes from the existing effective-summary reader.

A generation starts with no activities until an action actually begins. Activities come from persisted attempts and their lifecycle events, using bounded public titles/details. There are no future pending actions or model-generated internal plans. An attempt can be `running`, `done`, `error` or `cancelled`; start/finish timestamps refer to persisted execution, and redelivery does not create a synthetic attempt. A cancellation closes already-begun actions while preserving completed history.

Statuses include `pending`, `queued`, `running`, `retry_waiting`, `completed`, `failed` and `cancelled`. Stages before the run exists are factual task/job stages; a live canonical generation uses `text_summary`, `text_ready`, `visual_enrichment` and `finalizing`. A retryable queue failure records the waiting fact and retains safe completed checkpoints. A terminal/dead queue failure and its run closure commit together.

Text becomes ready before optional visual work. Visual state is `not_requested`, `pending`, `running`, `complete`, `skipped`, `failed` or `cancelled`. A readable text result survives an unavailable capability, unsuitable frames or visual provider failure. `fallback_reason` uses bounded public codes such as `vision_unavailable`, `visual_not_beneficial`, `visual_budget_exhausted`, `requested_visual_mode_unavailable`, `invalid_visual_plan` or `visual_enrichment_failed`; raw provider errors are excluded. Requested and resolved modes remain distinct.

Legacy tasks return `legacy: true`, an empty generation ID, no activities and no invented event stream. Their stage/status and readable result reflect stored task/summary facts. `mindmap_enabled` is an optional boolean from this generation’s frozen intent; an explicit false hides the derived structure. Legacy omission preserves the reader’s existing display default. Optional source metadata is omitted when no frozen source exists. Raw objects, provider inputs, source text, checkpoints, cookies, tokens and profile credentials are never returned by this endpoint.

## Event pages

```json
{
  "generation_id": "accepted-generation-uuid",
  "events": [{
    "seq": 6,
    "type": "run.completed",
    "data": {
      "status": "completed",
      "text_state": "ready",
      "result_state": "ready",
      "visual_state": "skipped",
      "fallback_reason": "visual_not_beneficial",
      "requested_mode": "auto",
      "resolved_mode": "text",
      "generated_version": 1
    },
    "created_at": "2026-10-10T03:00:03Z"
  }],
  "high_watermark": 6,
  "next_after_seq": 6,
  "has_more": false,
  "cursor_gap": false
}
```

Read pages in ascending sequence, advance only to `next_after_seq`, and keep paging while `has_more` is true. A page is bounded by one locked run high-watermark. When caught up, the page is empty and the cursor stays unchanged. `cursor_gap: true` means the next expected durable sequence is missing; fetch a fresh snapshot and recover from its `event_high_watermark` instead of inventing activity transitions. A future cursor must also reset from a fresh snapshot after its 409 response. Polling this API supports disconnect/restart recovery; the contract does not promise SSE transport.

Public event types include `run.started`, `activity.started`, `activity.finished`, `run.text_ready`, `run.text_reused`, `run.visual_started`, `run.retry_waiting`, `run.completed`, `run.failed` and `run.cancelled`. Events serialize an explicit safe payload allowlist, not the AgentRun or raw journal. Source refresh atomically replaces the active pointer and cancels older live source generations; their history remains owned and readable, while stale workers cannot publish over the replacement.

Classification candidates are frozen with text publication and consumed by the existing tag subsystem. Disabled classification produces a completed no-op receipt. Invalid candidates or vocabulary conflicts preserve the text result and a durable failed classification receipt. Visual revision publication supersedes older pending tag intents and consumes only the final generated version.

## Explicit regeneration and retry

For tasks with an active source, manual summary requests accept a new generation, freeze that exact current source/media identity and typed generated base, and atomically update the current task intent alongside its independent dispatch job. Previous options and instruction are retained. The same explicitly selected profile ID is resolved again using its current configuration, current preference and effective budget for this new user action. Automatic retries keep the accepted job snapshot and fingerprint; they do not resolve a new default or create another generation.

Acceptance preserves the old generated document and user revision head. Publication failure restores the existing dispatch to durable retry backoff with its frozen generation intact. An automatic retry redispatches that identity. A task with a new processing intent but no active complete source returns 422 instead of consuming stale ASR rows. Tasks without a source/intent retain their legacy manual flow. Historical import records with no captured initial generation remain empty instead of inventing an identity from a newer task intent.


## Visual-only retry

A real user action can request another bounded visual attempt without regenerating text. The result must be a typed `text` document on the task's current active source. Existing illustrated documents, an active job, a changed generated original, replaced source/media, deleted task or foreign owner cannot be used as the base. The server preserves the old generated row and user revision head while the attempt runs. No download, ASR, text-generation call or classification recipe runs in this operation.

```http
POST /api/v1/media/task/123/summary/visual-retry
Authorization: Bearer <session-token>
Idempotency-Key: <fresh-key-for-this-user-click>
Content-Type: application/json
```

```json
{
  "expected_generation_id": "previous-result-generation-uuid",
  "expected_generated_version": 1,
  "expected_content_digest": "generated-original-sha256",
  "expected_source_id": "source-uuid",
  "expected_source_digest": "source-sha256",
  "authorize_new_visual_budget": true,
  "profile_id": 12,
  "output_mode": "image_text"
}
```

`profile_id` and `output_mode` are optional. Profile selection defaults to the previously accepted explicit ID and resolves its current owned configuration, preference and effective budget at acceptance; no default-profile substitution occurs. Supported modes are `auto`, `image_text` and `keyframes`. The budget authorization field must be true because this user click accepts a **new bounded visual attempt**. Refresh, polling and network redelivery cannot authorize another attempt. The UI should explain that the saved text is reused and that another bounded visual attempt may incur model usage.

The expected fields come from the generation snapshot's `result_generation_id`, `generated_version`, `generated_content_digest`, `generated_source_id` and `generated_source_digest`. They describe the generated original, not an effective user-edited document. `content_digest` may identify a user revision and must not be substituted for `generated_content_digest`.

A successful acceptance returns HTTP 202:

```json
{
  "task_id": 123,
  "generation_id": "new-visual-attempt-uuid",
  "parent_generation_id": "previous-result-generation-uuid",
  "operation": "visual_retry",
  "accepted": true
}
```

A repeated identical key returns this immutable attempt ID without dispatching again, even after completion or another regeneration. Changed content with the same key returns 409. Invalid JSON, missing/duplicate keys, incomplete expected identity or missing budget authorization return 400; ownership/deletion returns 404; active/stale/source/profile conflicts return 409; unavailable vision or disabled task/visual capability returns 422. New-attempt admission returns 503 `summary_generation_disabled` when v2 generation is disabled. No rejected acceptance receipt or dispatch commits.

The accepted job stores the immutable original document, expected generated version/hash/source/media and a new explicit budget. Old runs, steps, events and spend remain unchanged. A queue redispatch resumes the **same** attempt, frozen profile fingerprint, counters, checkpoints and original duration deadline. This explicit new attempt is distinct from an automatic retry; its receipt records the previous generation/spend rather than silently resetting an existing run.

Only exact private source/profile/recipe/argument matches can reuse successful planner or inspected-frame checkpoints. Invalid planner results and prior image selections are not reused. An old inspection is reauthorized against the immutable owned observation and raw-response hash, then parsed with the current observation parser; old rows stay unchanged and reuse consumes no new VLM call. Missing or mismatched evidence cannot be published.

The latest snapshot adds `operation`, `parent_generation_id`, `generated_content_digest`, `generated_source_id`, `generated_source_digest` and `visual_retry_available`. The availability flag requires a current typed text result, inactive summary job and permitted task visual mode; acceptance still checks ownership, capabilities, switches and all expected fields. During a queued/running visual retry, `generation_id` identifies the new attempt while `result_generation_id` identifies the old readable original. `text_state` and `result_state` are ready because this complete text is frozen and reused. The actual `run.text_reused` event and reuse activity record that fact; no fake text-publication action or future activities appear. The run enters `visual_retry`, then actual visual enrichment stages.

If visual work fails or is skipped, the old `AISummary` row remains byte-for-byte unchanged and the new run records the specific visual reason with text/result still ready. On success, screenshot registration and a same-source/media/generated-base CAS publish one new generated version in the same transaction. A failed CAS rolls screenshot registration back. The prior user revision/head remain byte-for-byte unchanged and the existing effective reader reports `needs_merge` where appropriate. Source changes, cancellation, deletion, stale leases and accepted-profile changes close execution/publication rights. Historical generations and results remain readable under their original owner/task permissions.
