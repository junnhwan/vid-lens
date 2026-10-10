# RAG projection publication contract (v8)

One task/model build freezes its media fingerprint, active text source ID/digest,
and `StartedAt` ownership. Existing relational CAS and source fences authorize
chunk persistence and final `indexed` completion. Remote vector work runs after
the short relational transaction releases its locks.

## Remote generation isolation

Every build receives a private UUID namespace. Its `projection_` vector IDs are
SHA-256 bound to that namespace, owner/task/model, source ID/digest, chunk index,
and content hash. Identical wording in a refreshed subtitle track, or a later
build of the same source, has a different projection identity.

Build publication only upserts its own IDs. It never deletes or replaces the
entire task/model vector scope. If an old remote write finishes after a new
build has completed, the old build writes separate physical rows and its final
relational source/ownership fence rejects completion. It cannot overwrite the
new vectors or revert the current index state.

Physical orphan generations can remain after cancelled, failed, or retired
builds. No build cleanup deletes another generation. Explicit task cleanup
retains the full tenant/task/model deletion API. The maintenance-only
`ReplaceTaskChunks` API is excluded from the concurrent build path.

## Retrieval authority before TopK

The pgvector adapter selects rows only when authoritative `video_chunks` has
the matching chunk ID, vector ID, owner, task, and embedding model. SQL applies
this `EXISTS` predicate before similarity ordering and `LIMIT`; orphan vectors
cannot occupy the TopK budget. `ListTaskVectorManifest` uses the same current
scope. `ListAllVectorManifest` exposes physical rows for maintenance/auditing.

Server, evaluation, and audit factories derive the vector connection from the
same `cfg.Database` as the relational repository. There is no separate vector
DSN or production schema setting. `SourceChunksTableName` defaults to
`video_chunks`; injected test connections may specify an isolated authority
table in their own database/schema. Missing authority returns the query error
and never falls back to unfiltered generations. Future retriever adapters must
enforce this same current-chunk scope before ranking and TopK.

For tasks using a text-source workflow, retrieval also revalidates current
relational IDs after vector and keyword recall. A source refreshed during
recall retires those candidates before they enter model context. Explicit
`source_read` chunk identity lookups likewise reject deleted old chunk IDs.

## Version and verification

`CurrentRAGIndexBuildVersion` is 8. A version-7 row marked `indexed` requires
rebuilding under the isolated projection contract, including rows whose old
writer already clobbered the remote projection. Existing compatibility checks
for historical version-3 coarse source mappings remain independently defined.

`rag_generation_order_test.go` uses real relational repositories plus a remote
fixture that supports destructive full-scope replacement. Channels force the
old write to arrive after source refresh and successful new completion. Tests
verify distinct IDs despite identical content, current index state, readable
latest TopK/retrieval, rejected old `source_read` IDs, and recall-time refresh.

Adapter SQL fixtures verify authorization precedes `ORDER BY`/`LIMIT`, current
manifest scope, and fail-closed missing-table errors. The opt-in PostgreSQL
integration fixture additionally inserts a higher-scoring late orphan and
requires the current generation to win TopK=1. Default fixture tests and race
checks do not establish live PostgreSQL acceptance; that opt-in test must run
against the configured test PostgreSQL instance.

On 2026-10-10, `TestPGVectorStoreIntegration` also passed against the existing
local `vidlens-postgres` server, database `vidlens_summary_test`, pgvector
0.8.6. It used two unique temporary tables and verified current TopK, current
manifest (two rows), physical manifest (four rows including the late orphan),
tenant isolation, and full task cleanup that removed its orphan while retaining
the other tenant. Both fixture tables were dropped; the post-run inventory
reported zero remaining `vidlens_pgvector_it_*` tables. This verifies the real
adapter SQL and persistence boundary, alongside the channel-ordered service
fixture; it does not claim a deployed multi-worker race exercise.
