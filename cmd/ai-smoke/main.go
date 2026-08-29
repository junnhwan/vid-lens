// Command ai-smoke makes direct, read-only smoke calls against the configured
// AI adapters. It deliberately bypasses the database and HTTP server so a
// provider contract can be verified without creating application data.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
)

type smokeRunner struct {
	ctx      context.Context
	factory  *ai.Factory
	profile  ai.Profile
	checks   int
	failures int
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	imagePath := flag.String("image", "", "用于 Vision 测试的本地图片路径")
	audioPath := flag.String("audio", "", "可选：用于 ASR 测试的本地音频路径")
	timeout := flag.Duration("timeout", 2*time.Minute, "所有调用的总超时时间")
	flag.Parse()

	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout 必须大于 0")
		os.Exit(2)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	runner := &smokeRunner{ctx: ctx, factory: ai.NewFactory(), profile: cfg.AI.Profile()}
	runner.testChat()
	runner.testEmbedding()
	runner.testRerank()
	runner.testVision(*imagePath)
	runner.testASR(*audioPath)

	if runner.checks == 0 {
		fmt.Fprintln(os.Stderr, "没有可执行的 AI 检查：请在 .env 配置 Chat、Embedding、Rerank 或 Vision")
		os.Exit(1)
	}
	if runner.failures > 0 {
		fmt.Fprintf(os.Stderr, "AI smoke failed: %d/%d 项失败\n", runner.failures, runner.checks)
		os.Exit(1)
	}
	fmt.Printf("AI smoke passed: %d 项\n", runner.checks)
}

func (r *smokeRunner) check(name string, configured bool, fn func() (string, error)) {
	if !configured {
		fmt.Printf("SKIP %-9s 未配置\n", name)
		return
	}
	r.checks++
	detail, err := fn()
	if err != nil {
		r.failures++
		fmt.Printf("FAIL %-9s %v\n", name, err)
		return
	}
	fmt.Printf("PASS %-9s %s\n", name, detail)
}

func (r *smokeRunner) testChat() {
	p := r.profile
	r.check("chat", anyConfigured(p.LLMBaseURL, p.LLMAPIKey, p.LLMModel), func() (string, error) {
		client, err := r.factory.NewChatClient(p)
		if err != nil {
			return "", err
		}
		answer, err := client.Chat(r.ctx, []ai.ChatMessage{
			{Role: "system", Content: "只回复 OK，不要解释。"},
			{Role: "user", Content: "VidLens adapter smoke test"},
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("model=%s response_chars=%d", p.LLMModel, len([]rune(answer))), nil
	})
}

func (r *smokeRunner) testEmbedding() {
	p := r.profile
	r.check("embedding", anyConfigured(p.EmbeddingEndpoint, p.EmbeddingAPIKey, p.EmbeddingModel), func() (string, error) {
		client, err := r.factory.NewEmbeddingClient(p)
		if err != nil {
			return "", err
		}
		vector, err := client.Embed(r.ctx, "VidLens adapter smoke test")
		if err != nil {
			return "", err
		}
		if p.EmbeddingDim > 0 && len(vector) != p.EmbeddingDim {
			return "", fmt.Errorf("dimension=%d，配置为 %d", len(vector), p.EmbeddingDim)
		}
		return fmt.Sprintf("model=%s dimension=%d", p.EmbeddingModel, len(vector)), nil
	})
}

func (r *smokeRunner) testRerank() {
	p := r.profile
	r.check("rerank", anyConfigured(p.RerankEndpoint, p.RerankAPIKey, p.RerankModel), func() (string, error) {
		client, err := r.factory.NewRerankClient(p)
		if err != nil {
			return "", err
		}
		results, err := client.Rerank(r.ctx, "database transaction", []string{
			"The database transaction commits atomically.",
			"The video is encoded as H.264.",
		}, 2)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("model=%s results=%d", p.RerankModel, len(results)), nil
	})
}

func (r *smokeRunner) testVision(imagePath string) {
	p := r.profile
	configured := anyConfigured(p.VisionBaseURL, p.VisionAPIKey, p.VisionModel)
	if strings.TrimSpace(imagePath) == "" {
		if configured {
			fmt.Println("SKIP vision    已配置，但未传 --image")
		} else {
			fmt.Println("SKIP vision    未配置")
		}
		return
	}
	r.check("vision", configured, func() (string, error) {
		client, err := r.factory.NewVisionClient(p)
		if err != nil {
			return "", err
		}
		caption, err := client.CaptionImage(r.ctx, imagePath, ai.DefaultVisionCaptionPrompt)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("model=%s response_chars=%d", p.VisionModel, len([]rune(caption))), nil
	})
}

func (r *smokeRunner) testASR(audioPath string) {
	p := r.profile
	if strings.TrimSpace(audioPath) == "" {
		fmt.Println("SKIP asr       未传 --audio")
		return
	}
	r.check("asr", anyConfigured(p.ASRBaseURL, p.ASRAPIKey, p.ASRModel), func() (string, error) {
		strategy, err := r.factory.NewASRStrategy(p)
		if err != nil {
			return "", err
		}
		text, err := strategy.Transcribe(r.ctx, audioPath)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("model=%s response_chars=%d", p.ASRModel, len([]rune(text))), nil
	})
}

func anyConfigured(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
