package dbmigration

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"vid-lens/internal/model"
)

func TestDigestTimeNormalizationUsesUTCWithoutMonotonicData(t *testing.T) {
	instantWithMonotonic := time.Now()
	instantUTC := instantWithMonotonic.Round(0).UTC()
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}

	fromUTC, err := normalizeDigestValue(instantUTC, digestValueTime)
	if err != nil {
		t.Fatalf("normalize UTC time: %v", err)
	}
	fromShanghai, err := normalizeDigestValue(instantWithMonotonic.In(shanghai), digestValueTime)
	if err != nil {
		t.Fatalf("normalize Shanghai time: %v", err)
	}
	if !bytes.Equal(fromUTC, fromShanghai) {
		t.Fatalf("same instant normalized differently:\nUTC: %q\nShanghai: %q", fromUTC, fromShanghai)
	}

	nullValue, err := normalizeDigestValue(nil, digestValueTime)
	if err != nil {
		t.Fatalf("normalize NULL time: %v", err)
	}
	zeroValue, err := normalizeDigestValue(time.Time{}, digestValueTime)
	if err != nil {
		t.Fatalf("normalize zero time: %v", err)
	}
	if bytes.Equal(nullValue, zeroValue) {
		t.Fatal("SQL NULL and zero time produced the same normalized value")
	}
}

func TestDigestNormalizesDeletedAtBytesJSONAndDecimalWithoutLosingType(t *testing.T) {
	deletedAt := time.Date(2026, 7, 17, 8, 9, 10, 123456789, time.FixedZone("CST", 8*60*60))
	fromDeletedAt, err := normalizeDigestValue(gorm.DeletedAt{Time: deletedAt, Valid: true}, digestValueTime)
	if err != nil {
		t.Fatalf("normalize valid DeletedAt: %v", err)
	}
	fromTime, err := normalizeDigestValue(deletedAt.UTC(), digestValueTime)
	if err != nil {
		t.Fatalf("normalize DeletedAt time: %v", err)
	}
	if !bytes.Equal(fromDeletedAt, fromTime) {
		t.Fatalf("DeletedAt and its time normalized differently: %q != %q", fromDeletedAt, fromTime)
	}

	invalidDeletedAt, err := normalizeDigestValue(gorm.DeletedAt{}, digestValueTime)
	if err != nil {
		t.Fatalf("normalize invalid DeletedAt: %v", err)
	}
	nullValue, err := normalizeDigestValue(nil, digestValueTime)
	if err != nil {
		t.Fatalf("normalize NULL: %v", err)
	}
	if !bytes.Equal(invalidDeletedAt, nullValue) {
		t.Fatalf("invalid DeletedAt = %q, want SQL NULL encoding %q", invalidDeletedAt, nullValue)
	}

	byteValue, err := normalizeDigestValue([]byte("same"), digestValueBytes)
	if err != nil {
		t.Fatalf("normalize bytes: %v", err)
	}
	stringValue, err := normalizeDigestValue("same", digestValueString)
	if err != nil {
		t.Fatalf("normalize string: %v", err)
	}
	if bytes.Equal(byteValue, stringValue) {
		t.Fatal("byte slice and string produced the same normalized value")
	}

	firstJSON, err := normalizeDigestValue(`{"b":2,"a":1}`, digestValueJSON)
	if err != nil {
		t.Fatalf("normalize first JSON: %v", err)
	}
	secondJSON, err := normalizeDigestValue(" {\n  \"a\": 1, \"b\": 2\n}", digestValueJSON)
	if err != nil {
		t.Fatalf("normalize second JSON: %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("equivalent JSON normalized differently: %q != %q", firstJSON, secondJSON)
	}
	jsonNull, err := normalizeDigestValue("null", digestValueJSON)
	if err != nil {
		t.Fatalf("normalize JSON null: %v", err)
	}
	if bytes.Equal(jsonNull, nullValue) {
		t.Fatal("JSON text null and SQL NULL produced the same normalized value")
	}

	decimalA, err := normalizeDigestValue("001.230000", digestValueDecimal)
	if err != nil {
		t.Fatalf("normalize first decimal: %v", err)
	}
	decimalB, err := normalizeDigestValue([]byte("1.23"), digestValueDecimal)
	if err != nil {
		t.Fatalf("normalize second decimal: %v", err)
	}
	if !bytes.Equal(decimalA, decimalB) {
		t.Fatalf("equivalent decimals normalized differently: %q != %q", decimalA, decimalB)
	}
}

func TestDigestTableColumnsIdentifiesChangedColumnWithoutExposingValues(t *testing.T) {
	first := newPreflightTestDB(t)
	second := newPreflightTestDBForDigest(t, "column_second")
	createdAt := time.Date(2026, 7, 17, 1, 2, 3, 456000000, time.UTC)
	firstUser := model.User{ID: 10, Username: "source-sensitive-name", PasswordHash: "same-hash", CreatedAt: createdAt, UpdatedAt: createdAt}
	secondUser := firstUser
	secondUser.Username = "target-sensitive-name"
	if err := first.Create(&firstUser).Error; err != nil {
		t.Fatalf("insert first column fixture: %v", err)
	}
	if err := second.Create(&secondUser).Error; err != nil {
		t.Fatalf("insert second column fixture: %v", err)
	}

	spec := Catalog()[0]
	firstDigests, err := DigestTableColumns(context.Background(), first, spec)
	if err != nil {
		t.Fatalf("digest first columns: %v", err)
	}
	secondDigests, err := DigestTableColumns(context.Background(), second, spec)
	if err != nil {
		t.Fatalf("digest second columns: %v", err)
	}
	if len(firstDigests) != len(secondDigests) || len(firstDigests) == 0 {
		t.Fatalf("column digest sizes = %d and %d, want same non-zero size", len(firstDigests), len(secondDigests))
	}

	changed := make([]string, 0)
	for i := range firstDigests {
		if firstDigests[i].Table != "users" || secondDigests[i].Table != "users" {
			t.Fatalf("column digest table = %q/%q, want users", firstDigests[i].Table, secondDigests[i].Table)
		}
		if firstDigests[i].Column != secondDigests[i].Column {
			t.Fatalf("column order differs at %d: %q != %q", i, firstDigests[i].Column, secondDigests[i].Column)
		}
		if firstDigests[i].RowCount != 1 || secondDigests[i].RowCount != 1 {
			t.Fatalf("column %q row counts = %d/%d, want 1/1", firstDigests[i].Column, firstDigests[i].RowCount, secondDigests[i].RowCount)
		}
		if len(firstDigests[i].SHA256) != 64 || len(secondDigests[i].SHA256) != 64 {
			t.Fatalf("column %q has non-SHA256 digest", firstDigests[i].Column)
		}
		if firstDigests[i].SHA256 != secondDigests[i].SHA256 {
			changed = append(changed, firstDigests[i].Column)
		}
		encoded := fmt.Sprintf("%+v %+v", firstDigests[i], secondDigests[i])
		if strings.Contains(encoded, "source-sensitive-name") || strings.Contains(encoded, "target-sensitive-name") {
			t.Fatalf("column digest leaked source value: %s", encoded)
		}
	}
	if strings.Join(changed, ",") != "username" {
		t.Fatalf("changed columns = %v, want [username]", changed)
	}
}

func TestDigestTableIsPrimaryKeyOrderedAndIncludesSoftDeletedRows(t *testing.T) {
	first := newPreflightTestDB(t)
	second := newPreflightTestDBForDigest(t, "second")
	createdAt := time.Date(2026, 7, 17, 1, 2, 3, 456000000, time.UTC)
	deletedAt := createdAt.Add(time.Hour)

	rows := []model.User{
		{ID: 20, Username: "deleted", PasswordHash: "hash-20", CreatedAt: createdAt, UpdatedAt: createdAt, DeletedAt: gorm.DeletedAt{Time: deletedAt, Valid: true}},
		{ID: 10, Username: "active", PasswordHash: "hash-10", CreatedAt: createdAt, UpdatedAt: createdAt},
	}
	if err := first.Unscoped().Create(&rows[0]).Error; err != nil {
		t.Fatalf("insert first database deleted user: %v", err)
	}
	if err := first.Unscoped().Create(&rows[1]).Error; err != nil {
		t.Fatalf("insert first database active user: %v", err)
	}
	if err := second.Unscoped().Create(&rows[1]).Error; err != nil {
		t.Fatalf("insert second database active user: %v", err)
	}
	if err := second.Unscoped().Create(&rows[0]).Error; err != nil {
		t.Fatalf("insert second database deleted user: %v", err)
	}

	spec := Catalog()[0]
	firstDigest, err := DigestTable(context.Background(), first, spec)
	if err != nil {
		t.Fatalf("digest first database: %v", err)
	}
	secondDigest, err := DigestTable(context.Background(), second, spec)
	if err != nil {
		t.Fatalf("digest second database: %v", err)
	}
	if firstDigest.RowCount != 2 {
		t.Fatalf("row count = %d, want 2 including soft-deleted row", firstDigest.RowCount)
	}
	if firstDigest.SHA256 == "" || len(firstDigest.SHA256) != 64 {
		t.Fatalf("SHA-256 = %q, want 64 hexadecimal characters", firstDigest.SHA256)
	}
	if firstDigest != secondDigest {
		t.Fatalf("insertion order changed digest:\nfirst: %+v\nsecond: %+v", firstDigest, secondDigest)
	}
}

func TestDigestTableDistinguishesNULLFromEmptyString(t *testing.T) {
	db := newPreflightTestDB(t)
	createdAt := time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC)
	if err := db.Create(&model.VideoTask{
		ID:        1,
		UserID:    1,
		FileMD5:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Filename:  "null-source.mp4",
		SourceURL: "placeholder",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}).Error; err != nil {
		t.Fatalf("insert NULL string fixture: %v", err)
	}
	if err := db.Exec("UPDATE video_tasks SET source_url = NULL WHERE id = ?", 1).Error; err != nil {
		t.Fatalf("set source_url to NULL: %v", err)
	}

	spec := tableSpecByNameForDigest(t, "video_tasks")
	nullDigest, err := DigestTable(context.Background(), db, spec)
	if err != nil {
		t.Fatalf("digest NULL fixture: %v", err)
	}
	if err := db.Model(&model.VideoTask{}).Where("id = ?", 1).UpdateColumn("source_url", "").Error; err != nil {
		t.Fatalf("replace NULL with empty string: %v", err)
	}
	emptyDigest, err := DigestTable(context.Background(), db, spec)
	if err != nil {
		t.Fatalf("digest empty-string fixture: %v", err)
	}
	if nullDigest.SHA256 == emptyDigest.SHA256 {
		t.Fatal("SQL NULL and empty string produced the same table digest")
	}
}

func newPreflightTestDBForDigest(t *testing.T, suffix string) *gorm.DB {
	t.Helper()
	originalName := t.Name()
	db, err := gorm.Open(sqliteOpenForDigest(originalName, suffix), &gorm.Config{})
	if err != nil {
		t.Fatalf("open digest test database: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		t.Fatalf("migrate digest test database: %v", err)
	}
	return db
}

func sqliteOpenForDigest(testName, suffix string) gorm.Dialector {
	return sqlite.Open("file:" + strings.ReplaceAll(testName+"_"+suffix, "/", "_") + "?mode=memory&cache=shared")
}

func tableSpecByNameForDigest(t *testing.T, name string) TableSpec {
	t.Helper()
	for _, spec := range Catalog() {
		if spec.Name == name {
			return spec
		}
	}
	t.Fatalf("catalog has no table %q", name)
	return TableSpec{}
}
