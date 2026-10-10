package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"vid-lens/internal/ai"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
	"vid-lens/internal/summarydoc"
)

func TestSummarySelectionChatSnapshotsFollowupAndOrdinaryQAReadOnly(t *testing.T) {
	_, db, doc, _ := documentEditFixture(t)
	repos := repository.NewRepositories(db)
	ctx := context.Background()
	session := model.ChatSession{UserID: 7, TaskID: 142, ScopeType: model.ChatScopeVideo}
	if err := repos.Chat.CreateSession(&session); err != nil {
		t.Fatal(err)
	}
	svc := NewChatService(repos, nil, ChatConfig{})
	version := int64(1)
	block, err := repos.SummaryBlockContext(ctx, 7, 142, "install", model.SummaryVersionRef{GeneratedVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SummaryContextRef{Kind: "summary_selection", TaskID: 142, VersionRef: block.VersionRef, DocumentDigest: block.DocumentDigest, BlockID: block.BlockID, BlockDigest: block.BlockDigest, TextStart: 0, TextEnd: len([]rune(block.CanonicalText)), Quote: block.CanonicalText}
	accepted, err := svc.PrepareContextRefs(ctx, ConversationRequest{UserID: 7, SessionID: session.ID, Question: "总结一下", ContextRefs: []model.SummaryContextRef{ref}})
	if err != nil {
		t.Fatal(err)
	}
	chat := &recordingChatClient{response: "这是摘要解释"}
	if _, err = svc.AskWithMode(accepted, ChatModeNatural, 7, session.ID, "总结一下", 1, nil, chat, ai.Profile{LLMModel: "fixture"}); err != nil {
		t.Fatal(err)
	}
	messages, err := repos.Chat.ListMessages(7, session.ID)
	if err != nil || messages[0].ContextAnnotationsJSON == nil {
		t.Fatal("missing frozen annotation", err)
	}
	effective, err := repos.SummaryRevision.Effective(ctx, 7, 142)
	if err != nil || effective.Version != 0 || effective.ContentDigest != block.DocumentDigest {
		t.Fatal("ordinary QA edited summary", err)
	}
	doc.Blocks[0].BodyMarkdown = "新版本正文，旧选段消失。"
	canonical, _ := summarydoc.CanonicalJSON(doc)
	digest, _ := summarydoc.Digest(doc)
	markdown, _ := summarydoc.Markdown(doc)
	if err = db.Model(&model.AISummary{}).Where("task_id=142").Updates(map[string]any{"document_json": string(canonical), "content_digest": digest, "content": markdown, "generated_version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	followup := &recordingChatClient{}
	if _, err = svc.AskWithMode(ctx, ChatModeNatural, 7, session.ID, "那这个怎么设置", 1, nil, followup, ai.Profile{LLMModel: "fixture"}); err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, msg := range followup.messages {
		all += msg.Content
	}
	if !strings.Contains(all, artifact.JSON(ref.Quote)) || !strings.Contains(all, "衍生摘要上下文") {
		t.Fatal("followup lost frozen quote/provenance")
	}
	if err = db.Delete(&model.VideoTask{}, 142).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = svc.loadRecentMessages(ctx, 7, session.ID, 6); err == nil {
		t.Fatal("history bypassed current access")
	}
}
func TestSummarySelectionRejectedBeforeAnyStreamEvent(t *testing.T) {
	_, db, _, _ := documentEditFixture(t)
	repos := repository.NewRepositories(db)
	svc := NewChatService(repos, nil, ChatConfig{})
	session := model.ChatSession{UserID: 7, TaskID: 142, ScopeType: model.ChatScopeVideo}
	if err := repos.Chat.CreateSession(&session); err != nil {
		t.Fatal(err)
	}
	execution := NewConversationExecution(svc, nil, stubConversationProfileProvider{profile: ai.Profile{LLMModel: "fixture"}}, stubConversationClientFactory{})
	v := int64(999)
	events := 0
	_, err := execution.Stream(context.Background(), ConversationRequest{Kind: ConversationKindChat, UserID: 7, SessionID: session.ID, Question: "总结一下", ContextRefs: []model.SummaryContextRef{{Kind: "summary_selection", TaskID: 142, VersionRef: model.SummaryVersionRef{GeneratedVersion: &v}, BlockID: "install", Quote: "forged", TextEnd: 6}}}, func(ConversationStreamEvent) error { events++; return nil })
	if err == nil || events != 0 {
		t.Fatal("stale selection emitted SSE", events, err)
	}
}
func TestSummaryDocumentSelectedBlockEditsFreezeScope(t *testing.T) {
	svc, _, base, chat := documentEditFixture(t)
	ctx := context.Background()
	view, err := svc.Edit(ctx, 7, 142, "selected-block-edit", SummaryEditInput{Instruction: "Old改为New", ExpectedRevision: 0, Mode: "preview", SelectedBlockIDs: []string{"install"}})
	if err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 || view.Document.Title != base.Title || view.Document.Overview != base.Overview || !strings.Contains(view.Document.Blocks[0].BodyMarkdown, "New") {
		t.Fatal("selected correction changed unselected blocks")
	}
	op, err := svc.repos.SummaryRevision.Operation(ctx, 7, 142, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	wrong := "unselected title"
	patch := summarydoc.Patch{BaseContentHash: op.BaseContentHash, BaseContentHashKind: summarydoc.HashKind, Operations: []summarydoc.Operation{{Op: summarydoc.OpUpdateDocumentTitle, Title: &wrong}}}
	if err = svc.repos.SummaryRevision.Propose(ctx, op.ID, artifact.JSON(patch)); err == nil {
		t.Fatal("repository accepted out of scope patch")
	}
	var ids []string
	if err = json.Unmarshal([]byte(op.SelectedBlockIDsJSON), &ids); err != nil || len(ids) != 1 || ids[0] != "install" {
		t.Fatal("scope not frozen", err)
	}
}

func TestSummaryEditSelectionRequiresExpectedBodyAndVersion(t *testing.T) {
	svc, db, _, chat := documentEditFixture(t)
	current, err := svc.Effective(context.Background(), 7, 142)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	input := SummaryEditInput{Instruction: "Old改为New", Mode: "preview", SelectedBlockIDs: []string{"install"}, ExpectedContentDigest: current.ContentDigest, ExpectedVersionRef: &model.SummaryVersionRef{GeneratedVersion: &version}}
	if err = db.Model(&model.AISummary{}).Where("task_id=142").Update("generated_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = svc.begin(context.Background(), 7, 142, "old-selected-version", input); err == nil {
		t.Fatal("stale generated version accepted")
	}
	version = 2
	input.ExpectedContentDigest = "old-digest"
	if _, err = svc.begin(context.Background(), 7, 142, "old-selected-body", input); err == nil {
		t.Fatal("stale selected digest accepted")
	}
	if chat.calls != 0 {
		t.Fatal("stale selection reached model")
	}
}

func TestSummarySelectionAgentFreezeResumeAndFinalPrompt(t *testing.T) {
	repos, task, session := newVideoAgentTestSession(t)
	ctx := context.Background()
	base := "摘要旧段落😀：连接池需要按负载设置。"
	summary := model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: base, GeneratedVersion: 1}
	if err := repos.Summary.Create(&summary); err != nil {
		t.Fatal(err)
	}
	version := int64(1)
	block, err := repos.SummaryBlockContext(ctx, 7, task.ID, "legacy-0", model.SummaryVersionRef{GeneratedVersion: &version})
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SummaryContextRef{Kind: "summary_selection", TaskID: task.ID, VersionRef: block.VersionRef, DocumentDigest: block.DocumentDigest, BlockID: block.BlockID, BlockDigest: block.BlockDigest, TextStart: 0, TextEnd: len([]rune(block.CanonicalText)), Quote: block.CanonicalText}
	retriever := &fakeRetriever{results: []RetrievedChunk{{TaskID: task.ID, EvidenceID: "ev-research-annotation", ChunkID: 1, ChunkIndex: 2, Content: "owner 校验证据"}}}
	chatSvc := NewChatService(repos, retriever, ChatConfig{TopK: 5, CandidateK: 5, MinScore: .3})
	agent := NewVideoAgentService(chatSvc)
	accepted, err := chatSvc.PrepareContextRefs(ctx, ConversationRequest{UserID: 7, SessionID: session.ID, Question: "请研究owner校验", ContextRefs: []model.SummaryContextRef{ref}})
	if err != nil {
		t.Fatal(err)
	}
	client := &scriptedChatClient{responses: []string{`{"done":false,"tool":"search_transcript","reason":"定位证据","arguments":{"question":"owner校验","top_k":1}}`, `{"done":false,"tool":"build_cited_answer","reason":"证据足够","arguments":{"question":"owner校验","intermediate":"已定位","citations":[{"task_id":1,"evidence_id":"ev-research-annotation","chunk_id":1,"chunk_index":2,"content":"owner 校验证据"}]}}`, "回答[C1]", `{"done":true,"stop_reason":"完成"}`}}
	result, err := agent.RunAgent(accepted, VideoAgentLoopRequest{UserID: 7, SessionID: session.ID, Goal: "请研究owner校验", RunID: "annotation-resume"}, &fakeEmbeddingClient{dim: 3}, client, ai.Profile{EmbeddingModel: "text-embedding-3-small", LLMModel: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	records, err := repos.AgentExecution.GetExecution(ctx, 7, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var policy frozenAgentPolicy
	if err = json.Unmarshal([]byte(records.Run.PolicySnapshot), &policy); err != nil || len(policy.ContextAnnotations) != 1 || policy.ContextAnnotations[0].Quote != ref.Quote {
		t.Fatal("run snapshot missing accepted quote", err)
	}
	seenFinal := false
	for _, messages := range client.messages {
		for _, message := range messages {
			if strings.Contains(message.Content, "摘要选段（接受时冻结") && strings.Contains(message.Content, artifact.JSON(ref.Quote)) {
				seenFinal = true
			}
		}
	}
	if !seenFinal {
		t.Fatal("final answer model lost current annotation")
	}
	if err = repos.Summary.Upsert(&model.AISummary{TaskID: task.ID, FileMD5: task.FileMD5, Content: "新的摘要正文。", ModelName: "fixture"}); err != nil {
		t.Fatal(err)
	}
	resumed, err := chatSvc.PrepareContextRefs(ctx, ConversationRequest{UserID: 7, SessionID: session.ID, Question: "请研究owner校验", RunID: result.RunID})
	if err != nil || annotationsFromContext(resumed)[0].Quote != ref.Quote {
		t.Fatal("resume refreshed accepted annotation", err)
	}
	wrong := ref
	wrong.Quote = "替换"
	if _, err = chatSvc.PrepareContextRefs(ctx, ConversationRequest{UserID: 7, SessionID: session.ID, Question: "请研究owner校验", RunID: result.RunID, ContextRefs: []model.SummaryContextRef{wrong}}); err == nil {
		t.Fatal("resume accepted changed attachment")
	}
}
