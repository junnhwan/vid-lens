package model

const (
	VisualModeOff     = "off"
	VisualModeOCR     = "ocr"
	VisualModeCaption = "caption"
	VisualModeBoth    = "both"
)

func ValidVisualMode(mode string) bool {
	return mode == VisualModeOff || mode == VisualModeOCR || mode == VisualModeCaption || mode == VisualModeBoth
}

// EffectiveVisualMode preserves the old opt-out setting and historical rows.
// Import services explicitly choose off for new videos; migration never rewrites old evidence.
func (task *VideoTask) EffectiveVisualMode() string {
	if task == nil || task.VisualDisabled || task.VisualMode == VisualModeOff {
		return VisualModeOff
	}
	if task.VisualMode == VisualModeOCR || task.VisualMode == VisualModeCaption {
		return task.VisualMode
	}
	return VisualModeBoth
}

func (task *VideoTask) VisualOCRAllowed() bool {
	mode := task.EffectiveVisualMode()
	return mode == VisualModeOCR || mode == VisualModeBoth
}

func (task *VideoTask) VisualCaptionAllowed() bool {
	mode := task.EffectiveVisualMode()
	return mode == VisualModeCaption || mode == VisualModeBoth
}
