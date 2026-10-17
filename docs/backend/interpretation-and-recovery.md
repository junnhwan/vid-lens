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

The model's configured maximum output is a ceiling, not an output target. New article policies (`interpretation-2026-10-17-v6`, harness 7) cap investigation, chapter composition, repair, and explicit final submission separately (8,192 / 32,768 / 16,384 / 2,048 tokens, further limited by the frozen model profile and context reservation). Each call receives its actual output allowance and a suggested batch size. Increasing the context window alone does not remove an output cap. Q&A uses the composition allowance rather than the concise article's optional investigation allowance. Existing article runs and explicit retries retain their frozen policy.

New chapter writes accept 1–12 chapters, at most 128 KiB per batch and 512 KiB per saved draft. Chapters follow independent topics; related definitions, reasons, steps and examples belong together. The server independently validates each chapter and atomically saves the valid subset with an accepted/rejected receipt. A crash or replay returns that exact receipt; a rejected chapter cannot discard accepted siblings. The model corrects rejected IDs using the returned version, without rewriting accepted content. Draft corrections are durably bounded per draft version; planning corrections cannot exhaust a later chapter's allowance. Total call, time and no-progress limits still apply.

Once every planned chapter is saved, the server runs the same complete article/evidence/figure validator and immutable publication path automatically. It does not spend another model call just to return a version number. Failed final validation reopens only the necessary corrections and remains bounded; recovery uses the persisted draft and candidate. Optional `activity_title` metadata and explicit block/inline type enums match the new runtime contract. Plan repair advertises field replacement only, without the unsupported whole-object replacement option.

New private Q&A turns initialize harness 8, including short canonical video references and structured answer guidance, even when reading an older article. Existing initialized turn checkpoints retain their original protocol. Answers use paragraphs, lists, tables and inline emphasis according to the question, without injecting raw Markdown into structured text.

Optional article investigation enters wrap-up after 4 / 6 / 10 logical calls or 120 / 240 / 480 seconds of cumulative model-response time for concise / standard / detailed, or when at most eight calls remain. These are prompts to prioritize writing and limitations, not whole-task deadlines or permission to bypass outstanding critical evidence and final validation. Phase-specific tool schemas reduce irrelevant actions; a rejected draft retains the exact read and repair tools needed to address its validation errors.

Model profiles define `input_chars_limit` and `context_tokens` independently of output. Zero context tokens means unknown capacity. For imported combined-character profiles, an unset input limit preserves their previous effective allowance, `max_input_chars - 4 * max_output_tokens`; the selected phase's output no longer repeatedly consumes that envelope. Legacy checkpoints keep their previous path.

Text and image token reserves are explicitly heuristic and model dependent. Images still have the existing 12-frame/16 MiB working-set limits. A configured shared context limit checks text estimate + image reserve + selected output + safety reserve. This is not a provider tokenizer or billing measurement. Unreported provider input/output/cache usage remains unknown.

Before any physical model submission, the logical input, budget, tool schemas, and durable image references are saved. Worker takeover reuses that input. PostgreSQL JSON object key order cannot change its canonical argument hash. Incompatible old frozen tools fail explicitly instead of silently changing an already accepted operation.

## Subtitle adoption and diagnostics

Supported subtitle formats are selected before language preference can send an unsupported track to ASR. Extraction uses the metadata that supplied the track, tries at most three candidates, and places subtitle files in their own bounded directory. This avoids a second metadata selection and prevents a retained video from consuming a subtitle-only quota. Bilibili subtitle availability and manual/automatic/unknown kind are recorded separately.

Selection, download, parse outcome, cue coverage, fallback reason, and adopted text origin are durable source diagnostics. Coverage uses the existing cue-duration union thresholds: 50% usable and 85% complete. It does not infer speech or silence. ASR retains 60-second windows and coarse timing where the provider supplies no segments. Two workers use two globally fenced ASR lanes; completed windows commit independently, while transcript ordering and progress follow contiguous source windows. Recovery reuses completed windows, and cancellation joins workers before leaving. A rejected subtitle's language is not reported as an ASR language detection.

For YouTube without usable subtitles, bounded video and audio downloads can overlap in separate leased lanes. The combined download limit and full video verification still apply; prefetched audio is canceled when unnecessary. Source preparation, subtitles, video download, audio download, media verification and transcription have separately retained activity steps and elapsed times. Redis coordinates short-lived admission; PostgreSQL remains the authority for checkpoints, accepted drafts, observations and publication. Adding Redis copies of those records would not remove provider response latency.

Extractor metadata has a separate 32 MiB stdout allowance because many caption languages can produce more than 10 MiB of signed format/track URLs. Ordinary media-tool stdout remains bounded at 4 MiB. Any overflow fails explicitly; a truncated buffer is never parsed or published as a complete source. Metadata decode diagnostics retain size/limit and validity, without exposing signed URLs or raw extractor output.

Title and duration can appear before download and source publication. Failures include a safe diagnostic ID; HTTP responses include a generated `X-Request-ID`, also used in problem responses and linked to request/turn creation.
