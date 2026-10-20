package domain

// ExecutionContractV1 identifies the bootstrap/continuation adapter, separately
// from the user's immutable generation policy. Changing the default adapter
// requires a new version; already admitted runs retain this selection.
const ExecutionContractV1 = "vidlens-execution-2026-10-20-v1"

type ExecutionContract struct {
	Version string `json:"version"`
	RunID   string `json:"run_id"`
}
