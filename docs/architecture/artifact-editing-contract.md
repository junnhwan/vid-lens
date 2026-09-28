# Artifact editing — R1 contract

Contract version: 1 (2026-09-28). This document is the frontend/backend authority for R1 note editing. It extends `artifact-api-contract.md`; it does not change ordinary chat, manual save, answer import, learning-position, or generation semantics. All routes have the existing `/api/v1` prefix and require the existing owner-scoped JWT. Demo accounts remain read-only.

## Product authority and interfaces

An edit run is the only Agent execution authority. PostgreSQL stores its immutable request, frozen base version and scope, model/profile/budget snapshot, durable run state, tool records, and result. SSE or polling only observes that state; closing the page never cancels work.

Edit runs reuse the artifact worker's lease, budget, journal, recovery, and terminal-state machinery, but use their own durable `artifact_edit_dispatches` outbox and `vidlens.artifact.edit.v1` queue. Generation remains on `generation_dispatches` and `vidlens.artifact.generate.v1`. Publishers and consumers verify the persisted run subject before delivery/execution. This separation is part of the compatibility contract: an older generation-only worker in a rolling deployment cannot acknowledge and discard an edit message it does not understand.

The implementation is tested at four interfaces:

1. `artifact.EditPatch`: apply a bounded patch to an immutable `Body` and return a validated body plus a safe diff.
2. `ArtifactRepository`: submit/replay requests, publish or recover an operation transaction, apply a proposal, and undo without replacing later unrelated edits.
3. HTTP edit-run/operation routes: owner checks, request decoding, stable errors, and response unions.
4. The production artifact page: explicit full/block edit intent, progress, diff/evidence, conflict recovery, apply, and undo.

Manual `PATCH /artifacts/:id` remains human editing and keeps `origin=user`. Agent editing must not call or wrap that route. Ordinary paragraph questions continue to use the read-only chat path. If a user writes a question in the Agent edit panel, the planner may return an `answer` result, but no commit tool is registered or invoked for that result.

## Immutable edit request

`POST /artifacts/:id/edit-runs` requires `Idempotency-Key` and accepts:

```json
{
  "instruction": "把安装部分拆成步骤，保留命令和依据",
  "expected_head_version": 6,
  "selected_block_ids": ["install"],
  "mode": "apply"
}
```

- `instruction` is trimmed, 1–2,000 Unicode characters.
- `expected_head_version` must be the current positive head.
- `selected_block_ids` is an ordered, duplicate-free list of at most 20 current block IDs. An empty list authorizes the full artifact. A selected block authorizes that block and its current descendants; it does not authorize siblings, ancestors, or another artifact.
- `mode=answer|apply|preview` is chosen by the user action and frozen by the server. `answer` is read-only and cannot propose or commit a patch. Only an apply run can invoke `commit_artifact_patch`. A preview run can persist a proposal but cannot publish a version.
- The server freezes artifact ID, base version ID/number, manifest ID, scope, instruction, mode, profile fingerprint, budget, recipe, and allowed tool set. Model output cannot change owner, target, source, head, or scope.
- The request key is scoped to owner and edit-run submission. Same key plus the canonical payload returns the original run; same key with another payload returns `idempotency_conflict`.

Accepted runs return HTTP 202. `GET /artifact-edit-runs/:run_id` returns the owner-scoped run. `POST /artifact-edit-runs/:run_id/cancel` records cancellation intent and returns the current winner. `GET /artifact-edit-runs/:run_id/events` uses the existing durable cursor/SSE rules.

Artifact detail adds `latest_edit_run: EditRun|null`; it is separate from generation-only `latest_run`, so leaving and reopening the artifact does not lose a persisted edit.

## Planner tools and result union

The edit recipe is `study-edit-v1`. Current artifact data and a bounded set of frozen evidence are untrusted planner input, never instructions. Its edit-specific registry contains only the tools allowed by the frozen mode:

- `read_artifact`: read the bound base version, authorized block structure, hashes, and existing references. It cannot select another owner, artifact, version, or scope.
- `find_artifact_blocks`: locate text/structure only inside the bound base and authorized scope. It cannot search another artifact or expand selection.
- `inspect_artifact_evidence`: read a bounded set of evidence from the bound manifest; returned IDs remain server-owned.
- `answer_artifact_question`: answer/check the user's wording from the frozen content/evidence without a write.
- `propose_artifact_patch`: return one validated patch and a public summary.
- `nothing_to_change`: explain why no version is needed.

`mode=answer` registers only read/find/inspect/answer tools. `mode=preview` adds propose but no commit. For `mode=apply`, the runtime may invoke `commit_artifact_patch` only after `propose_artifact_patch` passes server validation. Evidence inspection is owner/manifest-bound and cannot create evidence identities. The default video-chat registry remains read-only and never receives an artifact commit tool. The product's ordinary “ask about this paragraph” action uses the existing read-only chat path (or an explicit answer-mode edit run); merely phrasing text as a question never grants write authority.

`EditRun.result` is a discriminated union:

- `{kind:"committed",operation_id,result_version_id}`
- `{kind:"proposal",operation_id}`
- `{kind:"answer",message,evidence_ids}`
- `{kind:"no_change",message,evidence_ids}`

An answer, no-change result, failed run, cancelled run, or exhausted run creates no version. A committed result is not reported until the operation exists durably. `cancel_requested=true` is not a terminal outcome.

## Structured patch

The planner never supplies JSON Patch, SQL, paths, HTML/JS, or executable layout code. A patch is bound to one artifact and base:

```json
{
  "schema_version": 1,
  "artifact_id": "artifact-id",
  "base_version_id": "version-id",
  "base_version": 6,
  "basis": "evidence_supported",
  "evidence_ids": ["evidence-id"],
  "operations": []
}
```

`basis=user_instruction|evidence_supported|evidence_conflict`. User-directed corrections may be saved without pretending the video proved them. `evidence_conflict` preserves and exposes the conflict. Evidence IDs must be accessible items in the frozen manifest.

Allowed operations:

- `update_title`: `{op,expected_hash,title}`. Full-artifact scope only.
- `update_block`: `{op,block_id,expected_hash,title?,content?,type?,evidence_refs?}`. Omitted fields and omitted references are byte-for-byte preserved.
- `insert_block`: `{op,key,parent_id,after_block_id,type,title,content,evidence_refs}`. `key` is a patch-local identity; the server derives the stable block ID from the durable operation ID and key.
- `delete_subtree`: `{op,block_id,expected_hash}`. Deleting the last root/body is invalid.
- `move_subtree`: `{op,block_id,expected_hash,parent_id,after_block_id}`. Cycles, orphan parents, and destinations outside a selected scope are invalid.
- `split_block`: `{op,block_id,expected_hash,parts:[...]}`. The target must be a leaf; 2–50 parts are allowed, the first part retains its block ID, and later part IDs are server-derived. References are explicit and validated.
- `merge_siblings`: `{op,block_ids,expected_hashes,title?}`. Between 2 and 50 adjacent leaf siblings are required. The first ID survives; content is concatenated with exact duplicate paragraphs removed, references are unioned, and no unique source text is discarded by a model rewrite.

Existing block preconditions use `sha256(canonical block JSON)`. Title uses the hash of its current string. One patch may contain at most 50 operations and may touch an existing block only once. Operations apply in order to an in-memory copy; the complete result must pass the existing 200-block, depth-8, content/body-size, parent-order, origin, and evidence validation before anything is written. Any invalid operation rejects the entire patch with `invalid_patch` or `target_scope_mismatch`.

Unchanged, out-of-scope blocks retain their exact JSON and relative order. Schema v1 requires parents to occur before descendants but does not require each subtree to be a contiguous DFS slice; leaf, subtree, sibling-anchor, move/delete, and undo logic therefore use parent identities rather than array adjacency. Existing references are preserved unless an authorized operation explicitly supplies a validated replacement. Existing `claim_origin=user` is never upgraded to a video-supported claim. A user-instruction edit is stamped `user`; an evidence-backed Agent rewrite is stamped `synthesis`. Original evidence text and historical versions are immutable.

## Operation, diff, and version contract

A durable operation stores owner, target, request/run, base version, manifest, canonical patch/hash, authorization scope, basis/evidence IDs, status, result version, undo result, idempotency records, and timestamps. Its ID is deterministically derived from the run and one fixed proposal slot, so preview, later apply, direct commit, and recovery refer to the same identity. New block IDs are deterministically derived from operation ID plus patch-local key.

`GET /artifact-edit-operations/:operation_id` follows target ownership and returns safe data:

```json
{
  "id": "operation-id",
  "artifact_id": "artifact-id",
  "status": "proposed",
  "base_version": 6,
  "base_version_id": "version-id",
  "result_version_id": null,
  "undo_version_id": null,
  "basis": "evidence_supported",
  "evidence_ids": ["evidence-id"],
  "summary": "拆成三个安装步骤",
  "counts": {"added":2,"updated":1,"deleted":0,"moved":0},
  "changes": [],
  "block_mappings": [],
  "can_apply": true,
  "can_undo": false
}
```

`changes` contains only changed title/block before/after snapshots and structural kind; the UI renders it as text and never reapplies it. `block_mappings` records stable split/merge lineage for selection and learning-position fallback. An Agent publication creates a new immutable artifact version with `origin=agent` and `edit_operation_id`. Undo creates another immutable version with `origin=undo`; it never deletes history. Version summaries and exports must accept both origins.

`POST /artifact-edit-operations/:id/apply` accepts `{expected_head_version}` plus `Idempotency-Key`. It applies a persisted proposal only to its frozen base. A moved head or changed/deleted source returns conflict; the client must start a new run after reading the new head.

`POST /artifact-edit-operations/:id/undo` accepts `{expected_head_version}` plus `Idempotency-Key`. If the operation result is still head, the inverse is applied as a new version. If later versions exist, undo changes only blocks whose current post-state still equals the operation result; unrelated later blocks are preserved. A later edit to any touched field, a new descendant under a block the operation added, or an unsafe structural anchor returns `undo_conflict`. Undo never restores a whole old snapshot over later work.

## Transaction, idempotency, and crash recovery

The write lock order is source → artifact → run/operation, matching source deletion.

For a new direct commit, one PostgreSQL transaction:

1. Rechecks owner, current source existence/hash, live run lease/epoch, cancellation, frozen target/scope, head CAS, and every patch precondition.
2. Applies and validates the complete patch in memory.
3. Inserts one immutable version, evidence rows, and the operation result, then advances artifact head.

The operation lookup by deterministic ID and patch hash occurs before the head check. A replay of a committed operation returns its original result even if head later moved. A different payload for the identity returns `idempotency_conflict`.

The tool checkpoint and terminal run event may be recorded after that transaction. If the worker stops in this gap, recovery checks the deterministic operation before acquiring a normal execution lease or marking running steps ambiguous, records the missing tool/result checkpoint, and completes the run without another version. Recovery and cancellation also check a committed operation before honoring a pending cancel intent. A committed operation therefore wins a later cancel request; the UI says saved and offers undo. If cancellation or lease loss wins before the transaction, no version or committed operation is created. A disconnected observer has no cancellation effect.

Preview persistence records a `proposed` operation bound to base/patch hash without moving head. Apply rechecks all write conditions. Apply and undo hash the action into their payload but share one owner-scoped outcome-key namespace, so clients must use a distinct key for each action. A same-key replay of the same action and payload returns the original version; reuse for another action or payload is rejected.

## Stable errors

| HTTP | `error_code` | Meaning |
| --- | --- | --- |
| 400 | `invalid_request`, `invalid_patch`, `invalid_evidence` | malformed request, operation, body, or evidence |
| 404 | `not_found` | absent or not owned |
| 409 | `idempotency_conflict`, `version_conflict`, `target_scope_mismatch`, `source_changed`, `undo_conflict`, `preview_expired` | state must be reconciled; no blind retry |
| 410 | `source_deleted` | source-gated content is no longer writable/readable |
| 422 | `profile_required`, `source_limit_exceeded`, `budget_exhausted` | configuration or bounded execution prevents the run |

Provider/internal text is not returned. `nothing_to_change` is a successful result kind, not an error. Source deletion cancels active edit runs and clears private checkpoints while leaving immutable metadata/history gated by the existing rules. Normal retention pruning may remove planner/recovery checkpoints, but retains the completed, public-safe `answer`/`no_change` result payload so an old completed run does not become result-less; source deletion removes that payload as well.

## Compatibility and verification

R1 keeps body schema version 1, the existing Markmap projection, manual save, answer import, generation, learning position, and Markdown export. It adds version origins and operation metadata only; old versions remain readable. No summary rule, editable canvas, new Agent framework, or raw transcript/vector rewrite is part of this contract.

The later R3 schema-2 relation and independent layout extension is documented in [the editable canvas contract](artifact-canvas-contract.md).

Required verification is tracked in `docs-private/agent-editing-canvas-acceptance-2026-09-28.md`. Unit/handler tests cannot substitute for PostgreSQL transaction/recovery tests, a real-model edit tool call, or the production Vite page.
