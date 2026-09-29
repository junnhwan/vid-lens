package model

import "testing"

func TestVisualModeKeepsLegacyOptOutAndSelectedProviders(t *testing.T) {
	for _, tc := range []struct {
		name         string
		task         VideoTask
		mode         string
		ocr, caption bool
	}{
		{"legacy", VideoTask{}, VisualModeBoth, true, true},
		{"legacy-disabled", VideoTask{VisualDisabled: true}, VisualModeOff, false, false},
		{"off", VideoTask{VisualMode: VisualModeOff}, VisualModeOff, false, false},
		{"ocr", VideoTask{VisualMode: VisualModeOCR}, VisualModeOCR, true, false},
		{"caption", VideoTask{VisualMode: VisualModeCaption}, VisualModeCaption, false, true},
		{"both", VideoTask{VisualMode: VisualModeBoth}, VisualModeBoth, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.task.EffectiveVisualMode() != tc.mode || tc.task.VisualOCRAllowed() != tc.ocr || tc.task.VisualCaptionAllowed() != tc.caption {
				t.Fatalf("mode=%s OCR=%v caption=%v", tc.task.EffectiveVisualMode(), tc.task.VisualOCRAllowed(), tc.task.VisualCaptionAllowed())
			}
		})
	}
}
