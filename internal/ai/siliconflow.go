package ai

// SiliconFlowStrategy is retained as a source-compatible name for callers
// that still construct the old strategy directly. SiliconFlow's standard
// chat, embedding, and audio-transcription endpoints now use the generic
// OpenAI-compatible adapters; the vendor name is no longer an implementation
// seam.
type SiliconFlowStrategy = OpenAICompatibleStrategy

// NewSiliconFlowStrategy is deprecated. Use NewOpenAICompatibleStrategy for
// new code. Keeping this constructor avoids breaking old integrations while
// removing SiliconFlow-specific behaviour from the implementation.
func NewSiliconFlowStrategy(apiKey, baseURL, asrModel, llmModel string) *OpenAICompatibleStrategy {
	return NewOpenAICompatibleStrategy(apiKey, baseURL, asrModel, llmModel)
}
