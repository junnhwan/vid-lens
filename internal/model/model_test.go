package model

import (
	"reflect"
	"sync"
	"testing"

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
