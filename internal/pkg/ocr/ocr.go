package ocr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Recognizer runs a local OCR CLI (default: tesseract) against an image file.
// Product path: extract on-screen text (PPT/board) so RAG can find content ASR misses.
type Recognizer struct {
	Command string
	Lang    string
	Timeout time.Duration
	run     func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func NewRecognizer(command, lang string) *Recognizer {
	if strings.TrimSpace(command) == "" {
		command = "tesseract"
	}
	if strings.TrimSpace(lang) == "" {
		lang = "chi_sim+eng"
	}
	return &Recognizer{
		Command: command,
		Lang:    lang,
		Timeout: 2 * time.Minute,
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				return nil, fmt.Errorf("ocr command failed: %s", msg)
			}
			return stdout.Bytes(), nil
		},
	}
}

// Available reports whether the configured OCR binary can be executed.
func (r *Recognizer) Available(ctx context.Context) bool {
	if r == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := r.run(ctx, r.Command, "--version")
	return err == nil
}

// Recognize returns cleaned OCR text for one image path.
func (r *Recognizer) Recognize(ctx context.Context, imagePath string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("ocr recognizer is nil")
	}
	if strings.TrimSpace(imagePath) == "" {
		return "", fmt.Errorf("ocr image path is empty")
	}
	if _, err := os.Stat(imagePath); err != nil {
		return "", fmt.Errorf("ocr image not found: %w", err)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// stdout mode: tesseract image stdout -l lang
	out, err := r.run(runCtx, r.Command, imagePath, "stdout", "-l", r.Lang, "--psm", "6")
	if err != nil {
		return "", err
	}
	return cleanOCRText(string(out)), nil
}

func cleanOCRText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSpace(s)
	// Collapse runs of blank lines.
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
