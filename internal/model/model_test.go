package model

import (
	"reflect"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestAllModelsIncludesTaskCleanupJob(t *testing.T) {
	want := reflect.TypeOf(&TaskCleanupJob{})
	for _, candidate := range AllModels() {
		if reflect.TypeOf(candidate) == want {
			return
		}
	}
	t.Fatalf("AllModels() does not include %v", want)
}

func TestVideoRAGIndexManifestUsesVariableLengthHashColumn(t *testing.T) {
	parsed, err := schema.Parse(&VideoRAGIndex{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse VideoRAGIndex schema: %v", err)
	}
	field := parsed.FieldsByDBName["chunk_manifest_sha256"]
	if field == nil {
		t.Fatal("VideoRAGIndex schema has no chunk_manifest_sha256 field")
	}
	if got := field.TagSettings["TYPE"]; got != "varchar(64)" {
		t.Fatalf("chunk_manifest_sha256 type = %q, want varchar(64)", got)
	}
}

func TestAllModelsIncludesKnowledgeBaseModels(t *testing.T) {
	want := map[reflect.Type]bool{
		reflect.TypeOf(&KnowledgeBase{}):      false,
		reflect.TypeOf(&KnowledgeBaseVideo{}): false,
		reflect.TypeOf(&ChatMessageSource{}):  false,
	}
	for _, candidate := range AllModels() {
		if _, ok := want[reflect.TypeOf(candidate)]; ok {
			want[reflect.TypeOf(candidate)] = true
		}
	}
	for typ, found := range want {
		if !found {
			t.Errorf("AllModels() does not include %v", typ)
		}
	}
}

func TestLegacyModelsKeepHistoricalChatSessionContract(t *testing.T) {
	var found bool
	for _, candidate := range LegacyModels() {
		if reflect.TypeOf(candidate) != reflect.TypeOf(&LegacyChatSession{}) {
			continue
		}
		found = true
		parsed, err := schema.Parse(candidate, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse legacy chat session: %v", err)
		}
		if _, ok := parsed.FieldsByDBName["scope_type"]; ok {
			t.Fatal("legacy chat session unexpectedly requires scope_type")
		}
		if _, ok := parsed.FieldsByDBName["knowledge_base_id"]; ok {
			t.Fatal("legacy chat session unexpectedly requires knowledge_base_id")
		}
	}
	if !found {
		t.Fatal("LegacyModels() does not include LegacyChatSession")
	}
}

func TestMigrateBackfillsChatSessionScopeAndRejectsInvalidCombinations(t *testing.T) {
	db := newModelSQLiteTestDB(t)
	if err := db.Exec(`CREATE TABLE chat_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		task_id INTEGER NOT NULL,
		title VARCHAR(200),
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create historical chat_sessions: %v", err)
	}
	if err := db.Exec(`INSERT INTO chat_sessions (user_id, task_id, title) VALUES (1, 42, 'old')`).Error; err != nil {
		t.Fatalf("insert historical session: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	var session ChatSession
	if err := db.First(&session).Error; err != nil {
		t.Fatalf("load migrated session: %v", err)
	}
	if session.ScopeType != ChatScopeVideo || session.KnowledgeBaseID != 0 || session.TaskID != 42 {
		t.Fatalf("migrated session = %+v, want video scope with task 42", session)
	}
	if err := db.Create(&ChatSession{UserID: 1, ScopeType: ChatScopeKnowledgeBase, KnowledgeBaseID: 7}).Error; err != nil {
		t.Fatalf("create valid knowledge-base session: %v", err)
	}
	if err := db.Create(&ChatSession{UserID: 1, ScopeType: ChatScopeVideo, TaskID: 0}).Error; err == nil {
		t.Fatal("invalid video scope with task_id=0 was accepted")
	}
	if err := db.Create(&ChatSession{UserID: 1, ScopeType: ChatScopeKnowledgeBase, TaskID: 9, KnowledgeBaseID: 7}).Error; err == nil {
		t.Fatal("invalid knowledge-base scope with task_id>0 was accepted")
	}
}

func newModelSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}
