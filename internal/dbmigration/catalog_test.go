package dbmigration

import (
	"reflect"
	"testing"

	"vid-lens/internal/model"
)

func TestCatalogIncludesEveryLegacyModelExactlyOnce(t *testing.T) {
	specs := Catalog()
	if got, want := len(specs), 20; got != want {
		t.Fatalf("legacy catalog tables = %d, want %d", got, want)
	}

	legacyModelTypes := make(map[reflect.Type]struct{}, len(model.LegacyModels()))
	allModelTypes := make(map[reflect.Type]struct{}, len(model.LegacyModels()))
	for _, item := range model.LegacyModels() {
		typeOf := reflect.TypeOf(item)
		if typeOf == nil || typeOf.Kind() != reflect.Ptr {
			t.Fatalf("LegacyModels contains non-pointer model %T", item)
		}
		if _, exists := allModelTypes[typeOf]; exists {
			t.Fatalf("LegacyModels contains duplicate model type %v", typeOf)
		}
		allModelTypes[typeOf] = struct{}{}
		legacyModelTypes[typeOf] = struct{}{}
	}
	if got, want := len(legacyModelTypes), len(specs); got != want {
		t.Fatalf("legacy runtime models = %d, catalog tables = %d", got, want)
	}

	catalogTypes := make(map[reflect.Type]string, len(specs))
	tableNames := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" {
			t.Fatal("catalog contains empty table name")
		}
		if _, exists := tableNames[spec.Name]; exists {
			t.Fatalf("catalog contains duplicate table %q", spec.Name)
		}
		tableNames[spec.Name] = struct{}{}

		typeOf := reflect.TypeOf(spec.Model)
		if typeOf == nil || typeOf.Kind() != reflect.Ptr {
			t.Fatalf("catalog table %q has non-pointer model %T", spec.Name, spec.Model)
		}
		if previous, exists := catalogTypes[typeOf]; exists {
			t.Fatalf("model %v appears in both %q and %q", typeOf, previous, spec.Name)
		}
		catalogTypes[typeOf] = spec.Name
		if _, exists := legacyModelTypes[typeOf]; !exists {
			t.Fatalf("catalog table %q uses non-legacy model %v", spec.Name, typeOf)
		}
		namer, ok := spec.Model.(interface{ TableName() string })
		if !ok {
			t.Fatalf("catalog model %v does not expose TableName", typeOf)
		}
		if got := namer.TableName(); got != spec.Name {
			t.Fatalf("catalog model %v reports table %q, want %q", typeOf, got, spec.Name)
		}
	}

	for typeOf := range legacyModelTypes {
		if _, exists := catalogTypes[typeOf]; !exists {
			t.Errorf("legacy runtime model %v is missing from catalog", typeOf)
		}
	}
}
func TestCatalogHasStableOrderAndCompleteKeyMetadata(t *testing.T) {
	specs := Catalog()
	wantOrder := []string{
		"users",
		"video_assets",
		"video_tasks",
		"task_jobs",
		"task_cleanup_jobs",
		"kafka_message_failures",
		"video_transcriptions",
		"video_transcription_chunks",
		"ai_summaries",
		"user_ai_profiles",
		"video_chunks",
		"video_rag_indexes",
		"chat_sessions",
		"chat_messages",
		"ai_call_logs",
		"ai_retry_budgets",
		"ai_retry_attempts",
		"ai_usage_ledgers",
		"quota_compensations",
		"user_usage_daily",
	}
	if len(specs) != len(wantOrder) {
		t.Fatalf("catalog tables = %d, want %d", len(specs), len(wantOrder))
	}

	seen := make(map[string]struct{}, len(specs))
	for i, spec := range specs {
		if spec.Name != wantOrder[i] {
			t.Errorf("catalog[%d] = %q, want %q", i, spec.Name, wantOrder[i])
		}
		if spec.PrimaryKey == "" {
			t.Errorf("catalog table %q has no primary key metadata", spec.Name)
		}
		if spec.AutoIncrement {
			if spec.SequenceName == "" {
				t.Errorf("auto-increment table %q has no PostgreSQL sequence metadata", spec.Name)
			}
		} else if spec.SequenceName != "" {
			t.Errorf("non-auto-increment table %q unexpectedly declares sequence %q", spec.Name, spec.SequenceName)
		}

		dependencyNames := make(map[string]struct{}, len(spec.Dependencies))
		for _, dependency := range spec.Dependencies {
			if dependency == spec.Name {
				t.Errorf("catalog table %q depends on itself", spec.Name)
			}
			if _, duplicate := dependencyNames[dependency]; duplicate {
				t.Errorf("catalog table %q repeats dependency %q", spec.Name, dependency)
			}
			dependencyNames[dependency] = struct{}{}
			if _, parentAlreadySeen := seen[dependency]; !parentAlreadySeen {
				t.Errorf("catalog table %q dependency %q is absent or ordered after its child", spec.Name, dependency)
			}
		}
		seen[spec.Name] = struct{}{}
	}

	budget := specs[15]
	if budget.Name != "ai_retry_budgets" || budget.PrimaryKey != "budget_id" || budget.AutoIncrement || budget.SequenceName != "" {
		t.Errorf("retry budget key metadata = %+v", budget)
	}
	for _, spec := range append(specs[:15], specs[16:]...) {
		if spec.PrimaryKey != "id" || !spec.AutoIncrement || spec.SequenceName != spec.Name+"_id_seq" {
			t.Errorf("table %q key metadata = pk %q auto=%v sequence=%q", spec.Name, spec.PrimaryKey, spec.AutoIncrement, spec.SequenceName)
		}
	}
}

func TestCatalogReturnsIndependentMetadata(t *testing.T) {
	first := Catalog()
	if len(first) == 0 {
		t.Fatal("catalog is empty")
	}
	first[0].Name = "mutated"
	if len(first[2].Dependencies) == 0 {
		t.Fatal("video_tasks dependencies are empty")
	}
	first[2].Dependencies[0] = "mutated"

	second := Catalog()
	if second[0].Name != "users" {
		t.Fatalf("catalog table metadata leaked mutation: %q", second[0].Name)
	}
	if second[2].Dependencies[0] == "mutated" {
		t.Fatal("catalog dependency metadata leaked mutation")
	}
	if reflect.ValueOf(first[0].Model).Pointer() == reflect.ValueOf(second[0].Model).Pointer() {
		t.Fatal("catalog returned shared mutable model pointer")
	}
}

func TestCatalogUsesLegacyChatSessionAndExcludesOnlineKnowledgeBaseTables(t *testing.T) {
	for _, spec := range Catalog() {
		switch spec.Name {
		case "knowledge_bases", "knowledge_base_videos", "chat_message_sources":
			t.Fatalf("legacy catalog unexpectedly contains online table %q", spec.Name)
		case "chat_sessions":
			if reflect.TypeOf(spec.Model) != reflect.TypeOf(&model.LegacyChatSession{}) {
				t.Fatalf("chat_sessions catalog model = %T, want *model.LegacyChatSession", spec.Model)
			}
		}
	}
}
