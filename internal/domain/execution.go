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

const CurrentExecutionContract = ExecutionContractV3

type ExecutionContract struct {
	Version string `json:"version"`
	RunID   string `json:"run_id"`
}
