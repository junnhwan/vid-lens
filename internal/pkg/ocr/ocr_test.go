package ocr

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCleanOCRTextDropsBlankLines(t *testing.T) {
	got := cleanOCRText("  前序遍历  \n\n\n根左右\r\n")
	want := "前序遍历\n根左右"
	if got != want {
		t.Fatalf("cleanOCRText() = %q, want %q", got, want)
	}
}

func TestRecognizeUsesStdoutMode(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotArgs []string
	r := NewRecognizer("tesseract", "chi_sim+eng")
	r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte("hello board\n"), nil
	}

	text, err := r.Recognize(context.Background(), imagePath)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if text != "hello board" {
		t.Fatalf("text = %q", text)
	}
	want := []string{"tesseract", imagePath, "stdout", "-l", "chi_sim+eng", "--psm", "6"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}
