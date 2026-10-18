package investigator

import (
	"bytes"
	"image/png"
	"testing"
)

func TestCapabilityProbeUsesOwnerSuppliedScreenshotAndChecksItsDetails(t *testing.T) {
	t.Run("private_screenshot", func(t *testing.T) {
		if capabilityProbeImageErr != nil {
			t.Skipf("private capability probe screenshot is not installed: %v", capabilityProbeImageErr)
		}
		config, err := png.DecodeConfig(bytes.NewReader(capabilityProbeImage))
		if err != nil {
			t.Fatalf("decode screenshot: %v", err)
		}
		if config.Width < 1500 || config.Height < 900 {
			t.Fatalf("expected the full detailed screenshot, got %dx%d", config.Width, config.Height)
		}
	})
	t.Run("observed_details", testCapabilityProbeScreenshotDetails)
}

func testCapabilityProbeScreenshotDetails(t *testing.T) {
	if !matchesCapabilityProbeObservation(expectedCapabilityProbeObservation) {
		t.Fatal("reference observation should pass")
	}
	wrong := expectedCapabilityProbeObservation
	wrong.PreviousDurationSeconds++
	if matchesCapabilityProbeObservation(wrong) {
		t.Fatal("a wrong detail from the previous check should fail")
	}
	wrong = expectedCapabilityProbeObservation
	wrong.Provider = "Other"
	if matchesCapabilityProbeObservation(wrong) {
		t.Fatal("a wrong provider should fail")
	}
}
