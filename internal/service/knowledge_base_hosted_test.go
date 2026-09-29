package service

import (
	"context"
	"errors"
	"testing"
	"vid-lens/internal/model"
)

func TestKnowledgeBaseUsesResolvedHostedEmbeddingModel(t *testing.T) {
	profiles, repos, db := hostedTestService(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	req := hostedTestRequest()
	if _, err := profiles.SaveHostedAdmin(2, req); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.ActivateHosted(3); err != nil {
		t.Fatal(err)
	}
	svc := NewKnowledgeBaseService(repos).WithAIProfiles(profiles)
	kb := createKnowledgeBaseForServiceTest(t, svc, 3, "hosted library")
	task := createKnowledgeBaseTaskForServiceTest(t, repos, 3, "hosted-index")
	createKnowledgeBaseRAGIndexForServiceTest(t, repos, 3, task.ID, task.FileMD5, req.EmbeddingModel, model.RAGIndexStatusIndexed)
	if err := svc.AddVideo(context.Background(), 3, kb.ID, task.ID); err != nil {
		t.Fatalf("already indexed hosted video rejected: %v", err)
	}
	view, err := svc.Get(context.Background(), 3, kb.ID)
	if err != nil || view.EmbeddingModel != req.EmbeddingModel || len(view.Videos) != 1 || !view.Videos[0].Retrievable {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if _, err := profiles.SaveHostedAdmin(2, HostedAIAdminRequest{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddVideo(context.Background(), 3, kb.ID, task.ID); !errors.Is(err, ErrHostedAIUnavailable) {
		t.Fatalf("paused hosted service must fail closed: %v", err)
	}
	view, err = svc.Get(context.Background(), 3, kb.ID)
	if err != nil || view.EmbeddingModel != "" || len(view.Videos) != 1 || view.Videos[0].TaskID != task.ID || view.Videos[0].Retrievable {
		t.Fatalf("existing library must remain readable while hosted AI is paused: %+v %v", view, err)
	}
}
