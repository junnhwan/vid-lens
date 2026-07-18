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

func TestAllModelsIncludesUploadSessionModels(t *testing.T) {
	wanted := map[reflect.Type]bool{
		reflect.TypeOf(&UploadSession{}):      false,
		reflect.TypeOf(&UploadSessionChunk{}): false,
	}
	for _, candidate := range AllModels() {
		if _, ok := wanted[reflect.TypeOf(candidate)]; ok {
			wanted[reflect.TypeOf(candidate)] = true
		}
	}
	for typeOf, found := range wanted {
		if !found {
			t.Errorf("AllModels() does not include %v", typeOf)
		}
	}
}
