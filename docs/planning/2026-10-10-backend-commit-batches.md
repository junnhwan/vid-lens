# Backend commit dependency batches (2026-10-10)

This is a file-level proposal over the current worktree, not a staged diff. No files were staged and no commits were made. The pre-existing pure summary document and Bilibili adapter commits are the base. All six intermediate boundaries compiled in a disposable `git archive HEAD` copy with only their explicit cumulative files applied: `go test ./cmd/server ./internal/service ./internal/repository ./internal/model ./internal/mq ./internal/handler -run '^$'` passed at each boundary. This verifies build closure; it does not replace behavioral tests below. No index or working-tree files were changed by the scratch checks.

Use these six review units in order. Batch 2 is larger because the transactional publication/cancellation/tag fences form a dependency cycle; reducing it requires explicit hunk staging or a preparatory refactor, not arbitrary file splitting. No unrelated runtime configuration is included.

## 1. Schemas and immutable value contracts

AllModels registration requires every new model in this batch. Processing options and source reconstruction are pure values. Goldmark is required by summaryselection.

```text
go.mod
go.sum
internal/model/artifact.go
internal/model/artifact_migration.go
internal/model/artifact_migration_postgres_test.go
internal/model/chat.go
internal/model/context_annotation.go
internal/model/import_request.go
internal/model/model.go
internal/model/rag_index.go
internal/model/summary.go
internal/model/summary_revision.go
internal/model/summary_screenshot.go
internal/model/summary_tag_intent.go
internal/model/tag_classification.go
internal/model/task.go
internal/model/task_job.go
internal/model/text_source.go
internal/model/transcription.go
internal/model/user_tag.go
internal/processing/intent.go
internal/processing/intent_test.go
internal/summaryselection/scope.go
internal/summaryselection/text.go
internal/summaryselection/text_test.go
internal/textsource/source.go
internal/textsource/source_test.go
internal/usertags/names.go
internal/usertags/names_test.go
```

## 2. Transactional persistence and publication

Full-file persistence closure is intentionally kept together: source publication cancels generation activities; generation publication prepares durable tag intent; summary revisions resolve source/screenshots; task lists resolve user tags; Repositories initializes source/import/tag stores. Splitting these files without carefully separating hunks creates undefined symbols or omits atomic fences.

```text
internal/repository/agent_execution.go
internal/repository/chat.go
internal/repository/download_identity.go
internal/repository/download_identity_test.go
internal/repository/import_request.go
internal/repository/import_request_test.go
internal/repository/rag_index.go
internal/repository/repository.go
internal/repository/source_cache_isolation_test.go
internal/repository/summary.go
internal/repository/summary_annotation.go
internal/repository/summary_annotation_test.go
internal/repository/summary_document.go
internal/repository/summary_document_test.go
internal/repository/summary_edit_expectation.go
internal/repository/summary_edit_expectation_test.go
internal/repository/summary_edit_scope.go
internal/repository/summary_generation.go
internal/repository/summary_generation_manual.go
internal/repository/summary_generation_read.go
internal/repository/summary_generation_source_status_test.go
internal/repository/summary_generation_test.go
internal/repository/summary_job.go
internal/repository/summary_revision.go
internal/repository/summary_revision_document_test.go
internal/repository/summary_screenshot.go
internal/repository/summary_screenshot_test.go
internal/repository/task.go
internal/repository/task_job_source.go
internal/repository/text_source.go
internal/repository/text_source_cancel_test.go
internal/repository/text_source_test.go
internal/repository/transcription.go
internal/repository/user_tag.go
internal/repository/user_tag_candidates.go
internal/repository/user_tag_intent.go
internal/repository/user_tag_intent_test.go
internal/repository/user_tag_merge.go
internal/repository/user_tag_test.go
internal/repository/user_tag_usage_test.go
```

## 3. Source readers, revisions, annotations and common execution

This batch supplies the actual source reader and provenance, journal extensions, AI-profile ownership helpers, annotation history authorization, typed edit activity and common visual investigator used by the pipeline. It depends on batches 1 and 2.

```text
internal/service/agent_execution.go
internal/service/ai_profile_budget.go
internal/service/artifact_workspace.go
internal/service/chat.go
internal/service/chat_ask.go
internal/service/chat_messages.go
internal/service/chat_prepare.go
internal/service/chat_recent.go
internal/service/chat_run_history.go
internal/service/chunk_provenance.go
internal/service/conversation_context.go
internal/service/conversation_execution.go
internal/service/conversation_profile_budget.go
internal/service/conversation_progress.go
internal/service/summary_activity_test.go
internal/service/summary_annotations.go
internal/service/summary_annotations_test.go
internal/service/summary_document_edit.go
internal/service/summary_document_edit_test.go
internal/service/summary_edit.go
internal/service/summary_edit_activity.go
internal/service/summary_edit_planner.go
internal/service/summary_frame_quality.go
internal/service/task_cleanup.go
internal/service/task_cleanup_source_test.go
internal/service/task_transcript_source.go
internal/service/text_source_read.go
internal/service/text_source_read_test.go
internal/service/user_tag.go
internal/service/video_agent.go
internal/service/video_agent_loop.go
internal/service/video_agent_loop_execution.go
internal/service/video_agent_loop_planner.go
internal/service/video_agent_loop_service.go
internal/service/video_agent_registry.go
internal/service/video_agent_tools.go
internal/service/video_questions.go
internal/service/video_timeline.go
internal/service/visual_investigator.go
```

## 4. Import and actual summary workers

Import, manual regeneration, frozen-profile workers and generation/visual service are mutually connected through media_tasks and summary_job. Keep their tests and MQ registries together. Existing routes remain usable until batch 5 adds the new read contracts and production generator wiring.

```text
internal/mq/consumer.go
internal/mq/consumer_analyze.go
internal/mq/consumer_download.go
internal/mq/consumer_summary.go
internal/mq/consumer_text_source.go
internal/mq/consumer_text_source_asr.go
internal/mq/consumer_text_source_asr_test.go
internal/mq/consumer_text_source_test.go
internal/mq/consumer_transcribe.go
internal/mq/download_source_identity.go
internal/mq/download_source_identity_test.go
internal/mq/frozen_processing_profile.go
internal/mq/producer.go
internal/mq/retry.go
internal/mq/transcription_workflow.go
internal/service/media.go
internal/service/media_chunk_upload.go
internal/service/media_import.go
internal/service/media_import_test.go
internal/service/media_summary_availability_test.go
internal/service/media_summary_generation.go
internal/service/media_summary_generation_test.go
internal/service/media_tasks.go
internal/service/media_url_upload.go
internal/service/summary_generation.go
internal/service/summary_generation_read.go
internal/service/summary_generation_read_test.go
internal/service/summary_generation_segments.go
internal/service/summary_generation_test.go
internal/service/summary_screenshot.go
internal/service/summary_visual.go
internal/service/summary_visual_test.go
```

## 5. Authenticated HTTP and production composition

The server uses the real handlers only once all service constructors exist. Keep router and wiring whole-file changes here, including tags/annotations/screenshots/generation routes; do not stage only the summary setter and accidentally omit adjacent registrations.

```text
cmd/server/router.go
cmd/server/wiring.go
docs/architecture/summary-annotations-contract.md
docs/architecture/summary-generation-contract.md
docs/architecture/user-tags-contract.md
docs/planning/2026-10-10-summary-integration-audit.md
docs/planning/2026-10-10-backend-commit-batches.md
internal/handler/chat.go
internal/handler/media.go
internal/handler/media_auto_import_test.go
internal/handler/summary_annotation.go
internal/handler/summary_generation.go
internal/handler/summary_generation_test.go
internal/handler/summary_revision.go
internal/handler/summary_screenshot.go
internal/handler/user_tag.go
internal/handler/user_tag_test.go
```

## 6. RAG projection generation and external-vector ordering

Generation-specific vector filters, before/after source fences and monotonic projection ordering must be shipped together. This batch belongs to the source agent; wait for its final race regression before integration.

```text
docs/planning/2026-10-10-rag-projection-generation.md
internal/service/rag_generation_order_test.go
internal/service/rag_index.go
internal/service/rag_index_build.go
internal/service/rag_index_guard_test.go
internal/service/rag_index_test.go
internal/service/rag_pipeline.go
internal/service/rag_projection_filter.go
internal/service/rag_source_refresh_test.go
internal/vector/cross_video_search_test.go
internal/vector/factory.go
internal/vector/factory_test.go
internal/vector/pgvector.go
internal/vector/pgvector_integration_test.go
internal/vector/pgvector_test.go
```

## Checks and exclusions

- Batch 1: `go test ./internal/model ./internal/processing ./internal/textsource ./internal/summaryselection ./internal/usertags`.
- Batch 2: `go test ./internal/repository`, then isolated PostgreSQL source/import/lease/publication/tag/revision regressions.
- Batches 3 and 4: targeted source, journal, import/manual generation and visual service tests; MQ source/download ownership and retry tests.
- Batch 5: `go test ./internal/handler ./cmd/server`, authenticated local routes and cursor/owner checks.
- Batch 6: RAG refresh-during-embed and concurrent external projection regressions plus pgvector integration tests.
- Then full `go test ./...`, frontend validation and isolated actual-server acceptance.

Excluded from these backend commits: frontend files, `docker-compose.yml` (local runtime configuration), and `VIDLENS_RESUME.md` (handoff state). Planning and acceptance documents should describe fixture, authenticated server and paid provider coverage separately.

Verified here: isolated PostgreSQL generation/tag rollback and import/lease atomic regressions passed. These results do not imply live provider output quality or browser acceptance.

Intermediate compile logs: `/tmp/vidlens-backend-batch-{1..6}-compile.log`; metadata `/tmp/vidlens-backend-batch-closure.json`. The first candidate exposed one missing dependency (`visual_investigator.go` calls `validateSummaryFrameQuality`); its helper now belongs to batch 3, and batches 3 through 6 were rechecked after this correction.
