package ai

import "context"

// Strategy AI 分析策略接口
// 面试亮点：策略模式 —— 语音转文字和大模型总结可独立替换
// 对比原项目 Java 版的 AiAnalysisStrategy，Go 用 interface 更简洁
type Strategy interface {
	// Transcribe 语音转文字（ASR）
	Transcribe(ctx context.Context, audioPath string) (string, error)

	// Summarize 大模型总结
	Summarize(ctx context.Context, text string) (string, error)
}
