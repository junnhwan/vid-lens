# Transcript dev regression and independent timing audit

`rag-eval transcript` is an offline, read-only evaluation command added for the first improvement round. It reads frozen ASR windows and predicted sentences; it does not request ASR, alignment, embeddings or an LLM, change published data, or populate human annotations. Outputs are new files (`O_EXCL`); existing reports cannot be overwritten.

```sh
mkdir -p artifacts/eval/transcript
go run ./cmd/rag-eval transcript \
  --dataset docs/eval/transcript-cases.dev.json \
  --identity <private-frozen-identity.json> \
  --output artifacts/eval/transcript/structure-run-01.json \
  --review-output artifacts/eval/transcript/review-run-01.html
```

All three main flags are required. `--help` describes the optional review output. The command exits unsuccessfully on invalid input or a structural violation; a structurally failed report is retained for inspection. Create parent directories before running. The HTML links to the local authorized video page and each predicted start time. Login and accessible original media are required for replay; the HTML itself does not submit annotations.

## Frozen input

Dataset `schema_version=1`, `split=dev`, and nonempty `cases` are required. Unknown fields and trailing JSON are rejected. Public synthetic cases are in [transcript-cases.dev.json](transcript-cases.dev.json). Private media, raw windows, annotation drafts and identities belong in `docs-private/eval/transcript/`; reports and review pages belong in `artifacts/eval/transcript/`.

Each case has a unique `id`, `kind` (`synthetic` or `media`), `tags`, positive `duration_ms`, and ordered `windows`. A media case also records a SHA-256 of the precise audio/video bytes being evaluated, and optionally an owner-accessible `task_id`. State explicitly whether the hash belongs to extracted audio or original video. Do not put media URLs or credentials in the dataset.

Windows freeze `chunk_index`, `segment_key`, `segmenter_version`, window/core millisecond boundaries, status, immutable ASR `content`, and optional `words`. Words use the existing absolute-millisecond `TranscriptionSegment` shape, including original-text rune offsets and method. `expect_alignment_rejected` is reserved for deliberate invalid-alignment fixtures. A synthetic `expected_text` is a constructed invariant, never a recognition gold label for real media.

Predicted `sentences` freeze unique sentence ID, text, ordered `source_ids`, nullable `start_ms`/`end_ms`, `time_status`, and a separate nullable `annotation`. Source IDs are retained for playback/audit attribution; the evaluator does not independently regenerate or verify every sentence-to-source ID against the production timeline. Freeze/export the production timeline separately and use existing source-map tests for that guarantee.

An independent human annotation is:

```json
{
  "text": "the independently checked spoken sentence",
  "start_allowed_ms": [1000, 1100],
  "end_allowed_ms": [2400, 2500],
  "reviewed_by": "actual reviewer",
  "reviewed_date": "2026-10-06",
  "source_checked": true,
  "complete_playback": true
}
```

Use the first/last audible spoken word for boundaries; ambiguous boundaries may use an allowed interval. `complete_playback` may be null until actually checked. No human review means `annotation: null`; do not copy observed ASR or generated text into human truth. A reviewer name with missing date/source confirmation/text/boundaries is rejected.

The separate identity is a string-valued JSON object requiring `code_commit`, `patch_sha256`, `asr_model`, `alignment_model`, `assembler_version`, `source_mapping_version`, `index_version`, `config_sha256`, `hardware`, `preprocessing_version`, and `annotation_sha256`. Use explicit `none`/`unverified` when appropriate. Include language, raw ASR/timeline hashes, statistics source hash, measured inference durations/resources or explicit unknown values. Credential-like field names are rejected; the caller remains responsible for ensuring values contain no credentials or private download URLs. A WIP identity should hash a sorted manifest of tracked changes and new source files, not just `git diff` (which omits untracked files).

## Metrics and limits

Structural checks call the production assembler and aligned-word validator. They require unchanged retained source text, valid source ranges, complete source reconstruction, expected rejection of bad mappings, and exact synthetic text. The fixed suite covers adjacent overlap, legitimate repeated speech, missing/empty/failed windows, non-overlap, silence, version discontinuity, English spaces, quantized seam words, invalid word maps and a 120-window synthetic long sequence. Structural acceptance requires zero violations. It does not measure real speech accuracy.

CER normalizes Unicode letters/numbers to lower case and ignores punctuation/whitespace. WER uses Unicode word runs separated by nonletters/nonnumbers; this is useful for English but is not a Chinese linguistic tokenizer. Rates use independently reviewed reference characters/word runs as denominators, including text mismatches.

Start/end errors include only reviewed, text-matched sentences with `exact` predicted times. Error is distance to the allowed interval (zero inside it). P50/P95 use nearest rank; max and sample count are explicit. Text mismatches are `unmatched`; absent human labels or nonexact/missing matched times are `unknown`. Do not discard these counts when comparing candidates.

Complete replay success divides human-approved full replays by explicitly reviewed replays; unreviewed playback is counted separately. Exact character coverage divides retained alphanumeric characters from windows with a completely valid aligned-word map by all retained alphanumeric characters. It measures mapping coverage, not timestamp accuracy or sentence coverage.

No reviewed denominator produces `null` metrics and `human_quality_status=not_audited`. `partial_audit`/`audited` describe audit coverage, not achievement of quality thresholds. Human quality thresholds and candidate comparisons belong to the later WP2B round. Model latency, memory usage and billing are metadata from a separately observed inference run; this offline command does not measure them.
