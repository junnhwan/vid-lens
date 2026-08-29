package ai

import "context"

// Transcriber is the narrow seam needed by the media pipeline. Keeping it
// separate from Strategy lets ASR use the standard audio protocol without
// manufacturing an unrelated summarizer implementation.
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
	TranscribeChunks(ctx context.Context, audioPaths []string) (string, error)
}

// Strategy AI 分析策略接口
// 面试亮点：策略模式 —— 语音转文字和大模型总结可独立替换
// 对比原项目 Java 版的 AiAnalysisStrategy，Go 用 interface 更简洁
type Strategy interface {
	// Transcribe 语音转文字（ASR）
	Transcribe(ctx context.Context, audioPath string) (string, error)

	// TranscribeChunks 分片语音转文字，规避单次 ASR 请求体限制。
	TranscribeChunks(ctx context.Context, audioPaths []string) (string, error)

	// Summarize 大模型总结
	Summarize(ctx context.Context, text string) (string, error)
}
