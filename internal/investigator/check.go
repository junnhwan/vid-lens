package investigator

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"vidlens/internal/config"
	"vidlens/internal/domain"
)

func (e *Engine) CheckModel(ctx context.Context, p config.Profile) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	model, err := e.baseModel(ctx, p)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			c := color.RGBA{R: 0, G: 190, B: 45, A: 255}
			if x >= 60 {
				c = color.RGBA{R: 140, G: 0, B: 180, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, img)
	used, submitted := false, false
	t, err := utils.InferEnhancedTool("inspect_test_pattern", "Get the test image pixels.", func(ctx context.Context, in struct{}) (*schema.ToolResult, error) {
		used = true
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: "请读取图片左右两个区域的颜色。"}, {Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: domain.Ptr(base64.StdEncoding.EncodeToString(encoded.Bytes())), MIMEType: "image/png"}}}}}, nil
	})
	if err != nil {
		return nil, err
	}
	final, err := utils.InferEnhancedTool("submit_probe", "Submit the two observed colors as a structured result.", func(ctx context.Context, in struct {
		Left  string `json:"left"`
		Right string `json:"right"`
	}) (*schema.ToolResult, error) {
		left, right := strings.ToLower(in.Left), strings.ToLower(in.Right)
		if !used || !(strings.Contains(left, "green") || strings.Contains(left, "绿")) || !(strings.Contains(right, "purple") || strings.Contains(right, "紫")) {
			return nil, errors.New("observed colors did not match actual pixels")
		}
		submitted = true
		return textResult(map[string]bool{"validated_submission": true}), nil
	})
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "capability_check", Model: &imageBridge{base: model}, Instruction: "先调用 inspect_test_pattern 获取真实图片。收到图片后调用 submit_probe，提交左右两半的主要颜色；不得猜测或自由文本收尾。", MaxIterations: 3, ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{t, final}, ExecuteSequentially: true}, ReturnDirectly: map[string]bool{"submit_probe": true}}})
	if err != nil {
		return nil, err
	}
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx, []*schema.Message{schema.UserMessage("测试工具调用以及主模型接收工具图片后作出观察。")})
	result := ""
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				return nil, err
			}
			if msg != nil && msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 {
				result = msg.Content
			}
		}
	}
	lower := strings.ToLower(result)
	_ = lower
	pass := used && submitted
	if !pass {
		return nil, errors.New("tool image capability check did not match actual pixels")
	}
	return map[string]any{"tool_call": true, "actual_image_input": true, "observation_matches_test_pixels": true, "validated_submission": true}, nil
}

type imageBridge struct{ base model.ToolCallingChatModel }

func (m *imageBridge) WithTools(ts []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	b, e := m.base.WithTools(ts)
	return &imageBridge{base: b}, e
}
func (m *imageBridge) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return receiveModelResponse(ctx, m.base, NormalizeToolImages(msgs), modelResponseLimits{Total: 180 * time.Second, First: 90 * time.Second, Idle: 60 * time.Second}, opts...)
}
func (m *imageBridge) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, msgs, opts...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{v}), nil
}
