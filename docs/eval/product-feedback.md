# Answer feedback and product regression candidates

Feedback is an owner-scoped assessment, not a correctness label. Only persisted assistant messages accept feedback. The server resolves a run from the message snapshot and verifies its owner/session; client-supplied run/model/budget facts are rejected.

Current wiring: `ChatWorkspace` renders `AnswerFeedback` for persisted assistant messages in both Chat and Agent history, including persisted limited/cancelled/failed answers. Streaming or unpersisted messages have no write entry. DEMO hides the write entry and retains server-side write protection. Expand “评价回答” to load the current assessment; reads are cached by account/session/message, can be retried, and saved assessments survive refresh. Changing identity resets local edit state.

Authenticated endpoints (under `/api/v1`):

- `PUT /chat/sessions/:session_id/messages/:message_id/feedback`: `{ "rating": "problem", "category": "citation", "note": "optional note" }`. Ratings are `helpful` or `problem`; helpful requires an empty category, problem requires `content`, `citation`, `incomplete`, or `slow`. Notes are limited to 2,000 Unicode characters. Updating replaces the same owner's assessment.
- `GET` on the same path returns the current assessment or `null`.
- `DELETE` on the same path clears it idempotently.
- `GET /chat/feedback/candidates?page=1&page_size=50` returns owner-only negative feedback, with page size at most 100. Observed answers, authoritative run/profile/budget identity and source asset hashes are kept separate from reviewed labels.

Export to a new path outside Git; `artifacts/` is ignored by the repository. `--output` is required, `--base-url` defaults to `http://127.0.0.1:8080`, and `--token-env` defaults to `VIDLENS_EVAL_TOKEN`, the environment variable holding the authorized bearer token. Create the output directory first: these commands create only the output file (`os.OpenFile` with `O_CREATE|O_EXCL`) and never create parent directories, so a missing directory fails the run.

```powershell
New-Item -ItemType Directory -Force -Path .\artifacts\eval\product | Out-Null
go run ./cmd/rag-eval product-candidates export --output artifacts/eval/product/feedback-candidates-run-01.json
```

Failed, cancelled, limited and pending-confirmation product runs can be collected without inventing a feedback record. With `--results`, the paired `--dataset` is also required:

```powershell
go run ./cmd/rag-eval product-candidates export --results artifacts/eval/product/report-run-01.json --dataset <accepted-cases.json> --output artifacts/eval/product/failure-candidates-run-01.json
```

The original dataset digest must match the report. The complete dependent turn sequence is retained, including turns not reached after a failure. All exports are immutable files; the CLI refuses overwrites, so each run needs a new file name, and it never retries execution POSTs.

A person must independently check the source and provide a separate review file with `candidate_id`, `candidate_sha256`, `reviewed_by`, `reviewed_date` (ISO date or RFC3339), `source_checked: true`, `annotation_version`, nonempty `required_points`, and nonempty `evidence_groups`. Each group represents one necessary fact and may contain alternative source IDs. `review_note` records limitations such as a transcript-only review. The digest binds the review to the exported candidate. Do not copy `observed_answer` into gold or populate the reviewer fields without an actual human review.

```powershell
go run ./cmd/rag-eval product-candidates accept --input artifacts/eval/product/failure-candidates-run-01.json --review <human-review.json> --session-id <new-isolated-session-id> --output artifacts/eval/product/accepted-cases.json
```

Acceptance produces only `split=dev` cases, retains review attribution, and excludes observed model output from gold. `--input`, `--review` and `--output` are required, and `--session-id` must be a positive session ID that differs from the candidate's original session; prepare one in the same authorized source scope. Acceptance does not assert the new answer is correct, authorize model training, or convert the sample into a blind/sealed test.

Regression execution uses `rag-eval product`, which requires `--dataset`, `--identity` and `--output` together — omitting any one of them fails before any request is sent. `--identity` is the frozen experiment identity file (code/patch/data/profile/schema/budget hashes) and must be valid JSON without credentials, and the bearer token comes from `--token-env`:

```powershell
go run ./cmd/rag-eval product --dataset artifacts/eval/product/accepted-cases.json --identity <frozen-identity.json> --output artifacts/eval/product/report-run-01.json
```

The report stores the `dataset_sha256` of the dataset it ran, which is the digest `product-candidates export --results` compares against.
