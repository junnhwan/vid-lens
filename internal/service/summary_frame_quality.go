package service

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

// This gate catches tiny, blank and nearly uniform captures. Semantic relevance
// still requires the actual visual observation and the selection decision.
func validateSummaryFrameQuality(data []byte) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("summary frame is not a readable image")
	}
	b := img.Bounds()
	if b.Dx() < 320 || b.Dy() < 180 {
		return fmt.Errorf("summary frame resolution is too small")
	}
	var sum, squares, n float64
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/32) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/32) {
			r, g, blue, _ := img.At(x, y).RGBA()
			v := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(blue)) / 257
			sum += v
			squares += v * v
			n++
		}
	}
	if n == 0 || math.Sqrt(max(0, squares/n-(sum/n)*(sum/n))) < 8 {
		return fmt.Errorf("summary frame is blank or has insufficient contrast")
	}
	return nil
}
