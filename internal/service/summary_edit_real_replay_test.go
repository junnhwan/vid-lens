//go:build real_llm

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	appdb "vid-lens/internal/database"
	"vid-lens/internal/model"
	"vid-lens/internal/pkg/secret"
	"vid-lens/internal/repository"
)

type summaryReplayChat struct {
	client  ai.ChatClient
	calls   int
	capture string
}

func (c *summaryReplayChat) Chat(ctx context.Context, messages []ai.ChatMessage) (string, error) {
	c.calls++
	raw, err := c.client.Chat(ctx, messages)
	if c.capture != "" && err == nil {
		if writeErr := os.WriteFile(filepath.Join(c.capture, fmt.Sprintf("response-%d.json", c.calls)), []byte(raw), 0600); writeErr != nil {
			return "", writeErr
		}
	}
	return raw, err
}

// Read only production source/profile; all operations and revisions use an
// isolated in-memory database. Never logs credentials or provider payloads.
func TestSummaryEditRealSourceReplay(t *testing.T) {
	path, id := os.Getenv("VIDLENS_SUMMARY_REPLAY_CONFIG"), os.Getenv("VIDLENS_SUMMARY_REPLAY_OPERATION")
	if path == "" || id == "" {
		t.Skip("set source config and operation ID to opt into real model replay")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	connection, err := appdb.OpenPostgres(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SQL.SetMaxOpenConns(1)
	prod := connection.GORM
	if err = prod.Exec("SET default_transaction_read_only=on").Error; err != nil {
		t.Fatal(err)
	}
	var source model.SummaryEditOperation
	if err = prod.First(&source, "id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	passphrase := cfg.Security.APIKeySecret
	if passphrase == "" {
		passphrase = cfg.JWT.Secret
	}
	codec, err := secret.NewCodecFromPassphrase(passphrase)
	if err != nil {
		t.Fatal(err)
	}
	profiles := NewAIProfileService(repository.NewRepositories(prod).AIProfile, codec, nil)
	row, err := profiles.repo.FindByIDForUser(source.UserID, source.ProfileID)
	if err != nil || row == nil {
		t.Fatal("source profile unavailable")
	}
	decrypted, err := profiles.decryptProfile(row)
	if err != nil {
		t.Fatal(err)
	}
	profile := providerFromDecrypted(decrypted)
	if profileFingerprint(profile) != source.ProfileFingerprint {
		t.Fatal("source profile has changed since failed operation")
	}
	client, err := ai.NewFactory().NewChatClient(*profile)
	if err != nil {
		t.Fatal(err)
	}
	capture := os.Getenv("VIDLENS_SUMMARY_REPLAY_CAPTURE")
	if capture != "" {
		if err = os.MkdirAll(capture, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(capture, "source.md"), []byte(source.BaseContent), 0600); err != nil {
			t.Fatal(err)
		}
	}
	chat := &summaryReplayChat{client: client, capture: capture}
	svc, db := summaryRepairService(t, source.BaseContent, chat)
	accepted, err := svc.Submit(ctx, 7, 42, "real-source-summary-replay", SummaryEditInput{Instruction: source.Instruction, Mode: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ExecuteSummaryEdit(ctx, accepted.RunID); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Operation(ctx, 7, 42, accepted.ID)
	if err != nil || view.Status != "proposed" {
		t.Fatalf("preview = %+v, %v", view, err)
	}
	if effective, err := svc.Effective(ctx, 7, 42); err != nil || effective.Content != source.BaseContent || effective.Version != 0 {
		t.Fatal("preview published content")
	}
	if _, err = svc.Apply(ctx, 7, 42, accepted.ID, 0); err != nil {
		t.Fatal(err)
	}
	effective, err := svc.Effective(ctx, 7, 42)
	if err != nil {
		t.Fatal(err)
	}
	from, to := os.Getenv("VIDLENS_SUMMARY_REPLAY_FROM"), os.Getenv("VIDLENS_SUMMARY_REPLAY_TO")
	if from == "" || to == "" || !strings.Contains(source.BaseContent, from) {
		t.Fatal("set the exact source term and corrected term for a scoped assertion")
	}
	if effective.Content != strings.ReplaceAll(source.BaseContent, from, to) {
		t.Fatalf("replay did not make exactly the requested term corrections: old_occurrences=%d new_occurrences=%d edits=%d calls=%d", strings.Count(effective.Content, from), strings.Count(effective.Content, to), len(view.Edits), chat.calls)
	}
	var shared model.AISummary
	if err = db.First(&shared, "task_id=?", 42).Error; err != nil || shared.Content != source.BaseContent {
		t.Fatal("shared original changed")
	}
	t.Logf("model=%s source_chars=%d corrected_occurrences=%d edits=%d calls=%d; isolated preview/apply passed; production source read only", profile.LLMModel, len([]rune(source.BaseContent)), strings.Count(source.BaseContent, from), len(view.Edits), chat.calls)
}
