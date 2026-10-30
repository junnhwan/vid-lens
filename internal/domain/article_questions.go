package domain

const ArticleQuestionsPolicy = "article-questions-2026-10-30-v1"

type SuggestedQuestion struct {
	Question  string `json:"question" jsonschema:"description=A specific, concise Chinese question a reader of this video may ask, at most 60 characters. Never a claim or a generic title template."`
	SectionID string `json:"section_id,omitempty" jsonschema:"description=An existing supplied section id, or empty for a cross-section question."`
}

type SuggestedQuestions struct {
	Items []SuggestedQuestion `json:"items" jsonschema:"minItems=3,maxItems=4,description=3-4 distinct useful questions grounded in this video's topics, favoring explanation, comparison and practical implications."`
}
