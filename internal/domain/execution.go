package domain

// ExecutionContractV1 identifies the bootstrap/continuation adapter, separately
// from the user's immutable generation policy. Changing the default adapter
// requires a new version; already admitted runs retain this selection.
const ExecutionContractV1 = "vidlens-execution-2026-10-20-v1"

// V2 keeps the authoring protocol but selects explicit, resumable review units.
// Only new runs and explicit retries select it; V1 checkpoints keep their rules.
const ExecutionContractV2 = "vidlens-execution-2026-10-20-v2"

// V3 assigns review modes from server-resolved citation modalities and supplies
// the actual investigation scope. V1/V2 runs retain their frozen review rules.
const ExecutionContractV3 = "vidlens-execution-2026-10-20-v3"

// V4 bounds source delivery and separates chronological source coverage from
// targeted evidence retrieval. Older contracts retain their frozen inputs.
const ExecutionContractV4 = "vidlens-execution-2026-10-20-v4"

// V5 sizes source navigation cards within both source-density and whole-ledger
// budgets. Chunk identities stay compatible with V4; admitted V4 runs stay frozen.
const ExecutionContractV5 = "vidlens-execution-2026-10-21-v5"

// V6 reviews an indivisible text/pixel claim once with all of its evidence.
// Older runs retain their separate modality obligations and frozen inputs.
const ExecutionContractV6 = "vidlens-execution-2026-10-21-v6"

const SourceIndexProtocolV1 = "source-index-2026-10-20-v1"

const CurrentExecutionContract = ExecutionContractV6

type ExecutionContract struct {
	Version string `json:"version"`
	RunID   string `json:"run_id"`
}
