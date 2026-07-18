package dbmigration

import (
	"reflect"

	"vid-lens/internal/model"
)

// TableSpec is the single source of truth for the tables copied by the
// MySQL-to-PostgreSQL migration. Dependencies express a safe insertion order;
// they intentionally exclude optional, cyclic back-references such as
// task_jobs.retry_budget_id <-> ai_retry_budgets.job_id. Those links are
// checked by relationship audits after both tables have been copied.
type TableSpec struct {
	Name          string
	Model         any
	PrimaryKey    string
	AutoIncrement bool
	SequenceName  string
	Dependencies  []string
}

var migrationCatalog = []TableSpec{
	autoIDTable("users", &model.User{}),
	autoIDTable("video_assets", &model.VideoAsset{}),
	withDependencies(autoIDTable("video_tasks", &model.VideoTask{}), "users", "video_assets"),
	withDependencies(autoIDTable("task_jobs", &model.TaskJob{}), "users", "video_tasks"),
	withDependencies(autoIDTable("task_cleanup_jobs", &model.TaskCleanupJob{}), "users", "video_assets", "video_tasks"),
	autoIDTable("kafka_message_failures", &model.KafkaMessageFailure{}),
	withDependencies(autoIDTable("video_transcriptions", &model.VideoTranscription{}), "video_tasks"),
	withDependencies(autoIDTable("video_transcription_chunks", &model.VideoTranscriptionChunk{}), "video_tasks"),
	withDependencies(autoIDTable("ai_summaries", &model.AISummary{}), "video_tasks"),
	withDependencies(autoIDTable("user_ai_profiles", &model.UserAIProfile{}), "users"),
	withDependencies(autoIDTable("video_chunks", &model.VideoChunk{}), "users", "video_tasks"),
	withDependencies(autoIDTable("video_rag_indexes", &model.VideoRAGIndex{}), "users", "video_tasks"),
	withDependencies(autoIDTable("chat_sessions", &model.ChatSession{}), "users", "video_tasks"),
	withDependencies(autoIDTable("chat_messages", &model.ChatMessage{}), "users", "chat_sessions"),
	withDependencies(autoIDTable("ai_call_logs", &model.AICallLog{}), "users", "video_tasks", "chat_sessions"),
	{
		Name:       "ai_retry_budgets",
		Model:      &model.AIRetryBudget{},
		PrimaryKey: "budget_id",
		Dependencies: []string{
			"video_tasks",
			"task_jobs",
		},
	},
	withDependencies(autoIDTable("ai_retry_attempts", &model.AIRetryAttempt{}), "ai_retry_budgets"),
	withDependencies(autoIDTable("ai_usage_ledgers", &model.AIUsageLedger{}), "users", "video_tasks"),
	withDependencies(autoIDTable("quota_compensations", &model.QuotaCompensation{}), "users", "ai_usage_ledgers"),
	withDependencies(autoIDTable("user_usage_daily", &model.UserUsageDaily{}), "users"),
}

func autoIDTable(name string, modelValue any) TableSpec {
	return TableSpec{
		Name:          name,
		Model:         modelValue,
		PrimaryKey:    "id",
		AutoIncrement: true,
		SequenceName:  name + "_id_seq",
	}
}

func withDependencies(spec TableSpec, dependencies ...string) TableSpec {
	spec.Dependencies = dependencies
	return spec
}

// Catalog returns an independent snapshot so callers and tests cannot mutate
// the package-level migration contract. Model pointers are cloned as well as
// slices because GORM may populate values passed to create/query operations.
func Catalog() []TableSpec {
	result := make([]TableSpec, len(migrationCatalog))
	for i, spec := range migrationCatalog {
		result[i] = spec
		result[i].Dependencies = append([]string(nil), spec.Dependencies...)
		modelType := reflect.TypeOf(spec.Model)
		result[i].Model = reflect.New(modelType.Elem()).Interface()
	}
	return result
}
