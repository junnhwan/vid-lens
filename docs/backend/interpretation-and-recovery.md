# Interpretation detail and recovery

New desktop requests accept `detail_level`: `concise`, `standard` (the default), or `detailed`. An empty or unknown value returns a field error. For example:

```json
{
  "video_url": "https://www.youtube.com/watch?v=abcdefghijk",
  "detail_level": "detailed"
}
```

All three levels read the full available transcript and require the same real-frame observation, citation, and publication checks. Detail changes information retention and explanation, with a bounded optional inspection allowance; it does not promise a fixed length, price, latency, or image count. A question can ask for more detail than its article.

GenerationSpec version 2 stores the actual policy, its content hash, detail, model revision, purpose, and source-pipeline revision in PostgreSQL. Requests and articles expose only safe detail metadata. An explicit retry inherits that specification. Historical results retain their original specification and show “原版解读”. Published articles remain immutable.

Omitted detail and explicit `standard` have the same new idempotency identity. Pre-detail URL-only and model/purpose keys can still replay their original accepted request for the historical default; those keys cannot upgrade to another detail level. Article reuse includes the complete frozen specification. Source preparation remains independent of detail and interpretation model.

## Bounded generation

The article outline records source ranges, distinct key points, and supporting `tN`/`fN` aliases. Ranges may overlap for different topics but must be ordered, stay inside the video, and jointly cover it without gaps. This is a structural coverage check; it is not a semantic quality score. Chapter writes remain bounded and versioned. Later writing receives excerpts from accepted canonical blocks to avoid repeating earlier chapters; excerpts never replace the complete video transcript.

Tool feedback and the next-call readiness capsule report pending observations, critical questions, comparison capacity, optional inspection allowance, and remaining calls. Multiple inspection calls in one response are rejected together before creating frames or questions, with a protocol response for each call. Successful extraction still requires observation of actual pixels.

Argument errors, prerequisites, unknown tools, repeated requests, and rejected candidates contribute to durable no-progress tracking. A changed activity title, network heartbeat, or duplicate observation does not count as new evidence. Suppliers' tool-call IDs are scoped to a logical model call before dispatch so repeated provider IDs cannot replay unrelated earlier tool results.

Rejected candidates stay outside the publishable `candidate` operation. `patch_article_draft` uses version CAS and a restricted replace-only JSON Pointer subset. `read_candidate_fields` and `repair_candidate` support bounded final-answer corrections by candidate ID and exact hash. At most 16 field replacements and 64 KiB of patch data are accepted per attempt. Syntax-invalid candidates at most 64 KiB may receive one complete replacement object per authorized repair attempt; larger invalid JSON is retained and never silently truncated. Repair attempts have a separate frozen bound and consume the original run's call/time limits. Every repaired result must pass full validation before publication.

## Model budgets

The model's configured maximum output is a ceiling, not an output target. Current policies cap investigation, chapter composition, repair, and final submission separately (8,192 / 32,768 / 16,384 / 4,096 tokens, further limited by the frozen model profile). Q&A uses the composition allowance rather than the concise article's optional investigation allowance.

Model profiles define `input_chars_limit` and `context_tokens` independently of output. Zero context tokens means unknown capacity. For imported combined-character profiles, an unset input limit preserves their previous effective allowance, `max_input_chars - 4 * max_output_tokens`; the selected phase's output no longer repeatedly consumes that envelope. Legacy checkpoints keep their previous path.

Text and image token reserves are explicitly heuristic and model dependent. Images still have the existing 12-frame/16 MiB working-set limits. A configured shared context limit checks text estimate + image reserve + selected output + safety reserve. This is not a provider tokenizer or billing measurement. Unreported provider input/output/cache usage remains unknown.

Before any physical model submission, the logical input, budget, tool schemas, and durable image references are saved. Worker takeover reuses that input. PostgreSQL JSON object key order cannot change its canonical argument hash. Incompatible old frozen tools fail explicitly instead of silently changing an already accepted operation.

## Subtitle adoption and diagnostics

Supported subtitle formats are selected before language preference can send an unsupported track to ASR. Extraction uses the metadata that supplied the track, tries at most three candidates, and places subtitle files in their own bounded directory. This avoids a second metadata selection and prevents a retained video from consuming a subtitle-only quota. Bilibili subtitle availability and manual/automatic/unknown kind are recorded separately.

Selection, download, parse outcome, cue coverage, fallback reason, and adopted text origin are durable source diagnostics. Coverage uses the existing cue-duration union thresholds: 50% usable and 85% complete. It does not infer speech or silence. ASR timing remains coarse where only audio-window timestamps are known. A rejected subtitle's language is not reported as an ASR language detection. ASR remains sequential until measurements and supplier limits justify a concurrency change.

Title and duration can appear before download and source publication. Failures include a safe diagnostic ID; HTTP responses include a generated `X-Request-ID`, also used in problem responses and linked to request/turn creation.
