# Model and local-tool admission (first improvement round)

`GET /api/v1/user/optional-capabilities` retains its existing rerank/alignment fields and adds `capabilities` and `actions`. Reading does not perform model probes. The action map is a user-scoped model/tool configuration projection, not a resource authorization token. Existing submit services still check ownership, complete source inputs, source versions and conflicting leases, and resolve the current default profile again. Task-specific source/revision projection remains a later WP4 change.

The model dependency matrix lives in `internal/ai/capabilities.go`: transcribe requires ASR; summary/study/revise require LLM; index requires Embedding; retrieval Chat/Agent require LLM and Embedding; caption requires Vision. Upload/playback require no AI model. OCR and alignment are local operations. Factories reject missing groups only when that client is needed. Composite analysis supports missing ASR/LLM groups and rejects the corresponding unsupported operation without constructing unrelated required clients.

Model state fields distinguish complete `configured` groups, deployment permission, nullable personal preference, availability, effective selection, and health. `health=unchecked` is deliberately conservative: current manual capability probes are not persisted into this projection. Availability is admission based on known configuration/dependencies, not proof that a provider responds. Known OCR executable absence is `dependency_missing`; OCR language packages, FFmpeg/Python modules and alignment weights still receive their existing execution checks. Full local dependency self-inspection belongs to WP6.

Rerank keeps its existing deployment connection policy, defaults off per user, and changes through the existing PATCH field `rerank_enabled`. Alignment remains manually submitted and never starts because its capability is available. Visual mode remains per video. Memory continues through the existing MemoryPolicyService and session/UI projection; this endpoint does not invent a second memory authorization rule. Missing model groups or paused hosted service do not block upload, local OCR or local alignment admission.

The frontend reads the server's model/tool action map, displays only the required capability names, caches by authenticated identity, and invalidates after profile/default changes. Successful reads cannot authorize later POSTs; a profile change before submission can deny the operation. Resource-dependent messages and source lifecycle remain in their existing services.

## Partial BYOK profile updates

Create accepts at least one complete group among LLM, ASR, Embedding and Vision. Every supplied group requires provider, address, key and model; Embedding also requires a positive dimension. Absent groups persist empty values/zero, never placeholder providers or dimensions, and empty ciphertext is not decrypted. This fits existing non-null database columns without migration. Full legacy profiles remain compatible; hosted profiles retain their existing read-only ownership rules.

Update semantics are explicit:

| Request | Behavior |
|---|---|
| Group omitted or all fields empty | Preserve its stored group |
| Existing group supplied with empty/omitted API key | Preserve stored encrypted key |
| New group supplied with no stored key | Reject |
| `clear_groups: ["asr"]`, with all ASR fields empty | Remove the ASR group |
| Clear and supply the same group, unknown/duplicate group, clear on create | Reject |
| Remove every group | Reject |

`name` and default-selection semantics remain as before. A newly edited form clearing unsaved fields does not send a server removal operation. No independent BYOK rerank credential group is added in this round.

Consumers skip automatic title generation when LLM is absent and automatic vector indexing when Embedding is absent. A successfully published ASR or local operation remains completed in those cases. Index refresh/invalidation and historical source identities continue using existing transactions; optional capability selection does not trigger downloads or model activation.
