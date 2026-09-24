package ai

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

// ProbeCapability sends a small request through the same admitted clients as production.
// The ASR input is silence: a valid empty transcript still proves the request was accepted.
func (t *ProfileTester) ProbeCapability(ctx context.Context, profile Profile, purpose string) (int, error) {
	switch purpose {
	case "llm":
		client, err := t.factory.NewChatClient(profile)
		if err != nil {
			return 0, err
		}
		_, err = client.Chat(ctx, []ChatMessage{{Role: "user", Content: "只回复：好"}})
		return 0, err
	case "embedding":
		client, err := t.factory.NewEmbeddingClient(profile)
		if err != nil {
			return 0, err
		}
		vector, err := client.Embed(ctx, "VidLens 模型探测")
		if err != nil {
			return 0, err
		}
		if len(vector) == 0 {
			return 0, fmt.Errorf("模型返回空向量")
		}
		if profile.EmbeddingDim > 0 && len(vector) != profile.EmbeddingDim {
			return len(vector), fmt.Errorf("向量维度不匹配：实际 %d，配置 %d", len(vector), profile.EmbeddingDim)
		}
		return len(vector), nil
	case "asr":
		file, err := os.CreateTemp("", "vidlens-probe-*.wav")
		if err != nil {
			return 0, fmt.Errorf("准备探测音频失败")
		}
		defer file.Close()
		defer os.Remove(file.Name())
		const samples = 8000
		var header [44]byte
		copy(header[:], "RIFF")
		binary.LittleEndian.PutUint32(header[4:], 36+samples*2)
		copy(header[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(header[16:], 16)
		binary.LittleEndian.PutUint16(header[20:], 1)
		binary.LittleEndian.PutUint16(header[22:], 1)
		binary.LittleEndian.PutUint32(header[24:], 8000)
		binary.LittleEndian.PutUint32(header[28:], 16000)
		binary.LittleEndian.PutUint16(header[32:], 2)
		binary.LittleEndian.PutUint16(header[34:], 16)
		copy(header[36:], "data")
		binary.LittleEndian.PutUint32(header[40:], samples*2)
		if _, err = file.Write(header[:]); err == nil {
			_, err = file.Write(make([]byte, samples*2))
		}
		if err == nil {
			err = file.Close()
		}
		if err != nil {
			return 0, fmt.Errorf("准备探测音频失败")
		}
		strategy, err := t.factory.NewASRStrategy(profile)
		if err != nil {
			return 0, err
		}
		_, err = strategy.Transcribe(ctx, file.Name())
		if err != nil && strings.Contains(err.Error(), "ASR 返回空结果") {
			return 0, nil
		}
		return 0, err
	case "vision":
		file, err := os.CreateTemp("", "vidlens-probe-*.png")
		if err != nil {
			return 0, fmt.Errorf("准备探测图片失败")
		}
		defer file.Close()
		defer os.Remove(file.Name())
		img := image.NewRGBA(image.Rect(0, 0, 128, 128))
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				img.Set(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
			}
		}
		if err = png.Encode(file, img); err == nil {
			err = file.Close()
		}
		if err != nil {
			return 0, fmt.Errorf("准备探测图片失败")
		}
		client, err := t.factory.NewVisionClient(profile)
		if err != nil {
			return 0, err
		}
		_, err = client.CaptionImage(ctx, file.Name(), "简述图片。")
		return 0, err
	default:
		return 0, fmt.Errorf("未知模型能力")
	}
}
