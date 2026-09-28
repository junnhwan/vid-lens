package artifact

import "math"

type CanvasPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type CanvasViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
type CanvasNode struct {
	Position  CanvasPoint `json:"position"`
	Width     float64     `json:"width"`
	Height    float64     `json:"height"`
	Pinned    bool        `json:"pinned"`
	Hidden    bool        `json:"hidden"`
	Collapsed bool        `json:"collapsed"`
	Style     string      `json:"style"`
}
type CanvasLayout struct {
	Direction string                `json:"direction"`
	Algorithm string                `json:"algorithm"`
	Nodes     map[string]CanvasNode `json:"nodes"`
	Viewport  CanvasViewport        `json:"viewport"`
}
type CanvasLayoutView struct {
	ContentVersionID string       `json:"content_version_id"`
	ViewID           string       `json:"view_id"`
	Revision         int64        `json:"revision"`
	Layout           CanvasLayout `json:"layout"`
}

func finiteBound(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}
func (layout CanvasLayout) Validate(body Body) error {
	if layout.Direction != "RIGHT" && layout.Direction != "DOWN" || layout.Algorithm != "elk-layered-v1" || layout.Nodes == nil || len(layout.Nodes) > 200 || !finiteBound(layout.Viewport.X, -100000, 100000) || !finiteBound(layout.Viewport.Y, -100000, 100000) || !finiteBound(layout.Viewport.Zoom, .1, 3) {
		return Err("invalid_layout", 400)
	}
	blocks := map[string]bool{}
	for _, block := range body.Blocks {
		blocks[block.BlockID] = true
	}
	for id, node := range layout.Nodes {
		if !blocks[id] || !finiteBound(node.Position.X, -100000, 100000) || !finiteBound(node.Position.Y, -100000, 100000) || !finiteBound(node.Width, 120, 600) || !finiteBound(node.Height, 60, 500) || node.Style != "auto" && node.Style != "section" && node.Style != "concept" && node.Style != "operation" && node.Style != "command" && node.Style != "note" {
			return Err("invalid_layout", 400)
		}
	}
	if len(JSON(layout)) > 128*1024 {
		return Err("invalid_layout", 400)
	}
	return nil
}
func DefaultCanvasLayout() CanvasLayout {
	return CanvasLayout{Direction: "RIGHT", Algorithm: "elk-layered-v1", Nodes: map[string]CanvasNode{}, Viewport: CanvasViewport{Zoom: 1}}
}
