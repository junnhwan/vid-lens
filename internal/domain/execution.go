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

// V7 routes incomplete semantic coverage back to bounded evidence acquisition,
// retaining actionable findings instead of losing a whole mixed-verdict batch.
const ExecutionContractV7 = "vidlens-execution-2026-10-21-v7"

// V8 gathers a complete text-review sweep before author correction, separates
// bound citations from contextual originals, and grows a truncated batch once.
const ExecutionContractV8 = "vidlens-execution-2026-10-21-v8"

// V9 projects semantically empty annotations and exact input-context echoes out
// of review responses before the unchanged strict identity/coverage checks,
// and bounds author feedback by affected fields rather than raw findings.
const ExecutionContractV9 = "vidlens-execution-2026-10-21-v9"

// V10 assigns article stages, evidence delivery and bounded recovery to the
// server. Existing runs keep their admitted adapter and exact frozen inputs.
const ExecutionContractV10 = "vidlens-workflow-2026-10-21-v1"

// V11 separates stage roles and persists chapter-scoped uncertainty through
// writing and revision. Admitted V10 runs retain their original wire contract.
const ExecutionContractV11 = "vidlens-workflow-2026-10-21-v2"

// V12 exposes the exact stage schema and an author shape before the first
// submission. Existing V10/V11 runs retain their admitted prompts and schema.
const ExecutionContractV12 = "vidlens-workflow-2026-10-21-v3"

const SourceIndexProtocolV1 = "source-index-2026-10-20-v1"

const CurrentExecutionContract = ExecutionContractV12

// ArticleLoopV1 is the first independent article adapter. Historical runs keep
// this identity; QA keeps its own adapter.
const ArticleLoopV1 = "vidlens-article-loop-2026-10-22-v1"

// V2 requires independently grounded pixel observations in the integrated
// review. V1 runs retain their admitted report schema and policy identity.
const ArticleLoopV2 = "vidlens-article-loop-2026-10-22-v2"

// Only newly admitted articles use this default. Restoring an existing run
// never replaces its frozen contract; legacy draft retries retain their adapter.
const CurrentArticleContract = ArticleLoopV2

func ArticleLoopContract(version string) bool {
	return version == ArticleLoopV1 || version == ArticleLoopV2
}

func StagedWorkflowContract(version string) bool {
	return version == ExecutionContractV10 || version == ExecutionContractV11 || version == ExecutionContractV12
}

// QuestionContract explicitly decouples the article writer from the existing
// interactive QA adapter, including image and external-reference questions.
func QuestionContract(articleVersion string, workflow, external bool) string {
	if external || ArticleLoopContract(articleVersion) || !workflow {
		return ExecutionContractV9
	}
	return ExecutionContractV12
}

type ExecutionContract struct {
	Version       string `json:"version"`
	RunID         string `json:"run_id"`
	InputRevision string `json:"input_revision,omitempty"`
}

const QuestionReadingV1 = "qa-article-reading-2026-10-22-v1"
const QuestionReadingV2 = "qa-article-reading-2026-10-22-v2"

func QuestionReadingContract(version string) bool {
	return version == QuestionReadingV1 || version == QuestionReadingV2
}

func QuestionInputRevision(articleVersion string) string {
	if articleVersion == ArticleLoopV2 {
		return QuestionReadingV2
	}
	return ""
}
