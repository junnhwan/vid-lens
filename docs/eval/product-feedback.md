# Answer feedback and product regression candidates

Feedback is an owner-scoped assessment, not a correctness label. Only persisted assistant messages accept feedback. The server resolves a run from the message snapshot and verifies its owner/session; client-supplied run/model/budget facts are rejected.

Authenticated endpoints (under `/api/v1`):

- `PUT /chat/sessions/:session_id/messages/:message_id/feedback`: `{ "rating": "problem", "category": "citation", "note": "optional note" }`. Ratings are `helpful` or `problem`; helpful requires an empty category, problem requires `content`, `citation`, `incomplete`, or `slow`. Notes are limited to 2,000 Unicode characters. Updating replaces the same owner's assessment.
- `GET` on the same path returns the current assessment or `null`.
- `DELETE` on the same path clears it idempotently.
- `GET /chat/feedback/candidates?page=1&page_size=50` returns owner-only negative feedback, with page size at most 100. Observed answers, authoritative run/profile/budget identity and source asset hashes are kept separate from reviewed labels.

Export to a new private path; `VIDLENS_EVAL_TOKEN` contains the authorized bearer token:

```powershell
go run ./cmd/rag-eval product-candidates export --output docs-private/eval/product/candidates/export.json
```

Failed, cancelled, limited and pending-confirmation product runs can be collected without inventing a feedback record:

```powershell
go run ./cmd/rag-eval product-candidates export --results <product-report.json> --dataset <original-cases.json> --output <new-private-candidates.json>
```

The original dataset digest must match the report. The complete dependent turn sequence is retained, including turns not reached after a failure. All exports are immutable files; the CLI refuses overwrites and never retries execution POSTs.

A person must independently check the source and provide a separate review file with `candidate_id`, `candidate_sha256`, `reviewed_by`, `reviewed_date` (ISO date or RFC3339), `source_checked: true`, `annotation_version`, nonempty `required_points`, and nonempty `evidence_groups`. Each group represents one necessary fact and may contain alternative source IDs. `review_note` records limitations such as a transcript-only review. The digest binds the review to the exported candidate. Do not copy `observed_answer` into gold or populate the reviewer fields without an actual human review.

```powershell
go run ./cmd/rag-eval product-candidates accept --input <export.json> --review <human-review.json> --session-id <new-isolated-session-id> --output <new-accepted-cases.json>
```

Acceptance produces only `split=dev` cases, retains review attribution, and excludes observed model output from gold. It requires a new evaluation session; prepare one in the same authorized source scope. Use `rag-eval product` for regression execution. Acceptance does not assert the new answer is correct, authorize model training, or convert the sample into a blind/sealed test.
