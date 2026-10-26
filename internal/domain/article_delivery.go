package domain

// Frozen at admission. Older runs retain their blocking review contract.
const ArticleDeliveryV1 = "delivery-first-2026-10-25-v1"

// V2 accepts creation keys in the same atomic save's chapter order. V1
// executions keep their original interface and failure evidence.
const ArticleDeliveryV2 = "delivery-first-2026-10-25-v2"

// V3 sizes the actual reading input and continues beyond saved source ranges.
const ArticleDeliveryV3 = "delivery-first-2026-10-25-v3"

// V4 keeps quote wording and optional figure quality advisory, and supplies
// the full saved author draft before spending calls on redundant rereads.
const ArticleDeliveryV4 = "delivery-first-2026-10-25-v4"

// V5 bounds recovery after malformed author JSON without dropping the saved
// draft or reusing a damaged response as the next writing template.
const ArticleDeliveryV5 = "delivery-first-2026-10-25-v5"

// V6 integrates optional visual requests into chapter saves, with durable
// capture/refinement limits and prose reserves. Older runs keep their wire.
const ArticleDeliveryV6 = "delivery-first-2026-10-25-v6"

// V7 restores concept maps in the author's final save. Already admitted runs
// keep their original wire and may finish without a map.
const ArticleDeliveryV7 = "delivery-first-2026-10-26-v7"
const CurrentArticleDelivery = ArticleDeliveryV7

func ArticleDeliveryPolicy(revision string) bool {
	return revision == ArticleDeliveryV1 || revision == ArticleDeliveryV2 || revision == ArticleDeliveryV3 || revision == ArticleDeliveryV4 || revision == ArticleDeliveryV5 || revision == ArticleDeliveryV6 || revision == ArticleDeliveryV7
}

type ArticleAdvice struct {
	Summary  string           `json:"summary"`
	Findings []ArticleFinding `json:"findings"`
}

type ArticleFinding struct {
	SectionID  string `json:"section_id"`
	Problem    string `json:"problem"`
	Suggestion string `json:"suggestion"`
}
