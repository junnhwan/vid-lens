package dbmigration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const (
	digestFormatVersion       = "vidlens-table-digest-v1"
	columnDigestFormatVersion = "vidlens-column-digest-v1"
)

type digestValueKind byte

const (
	digestValueString digestValueKind = iota + 1
	digestValueBytes
	digestValueBool
	digestValueInteger
	digestValueDecimal
	digestValueTime
	digestValueJSON
)

// TableDigest is a credential-free, deterministic summary of every persisted
// column and physical row in one catalog table. SHA256 is encoded as lowercase
// hexadecimal so reports remain easy to diff and consume from other tools.
type TableDigest struct {
	Table    string `json:"table"`
	RowCount int64  `json:"row_count"`
	SHA256   string `json:"sha256"`
}

// ColumnDigest is a value-free diagnostic summary used only to localize a
// table mismatch. It intentionally exposes no primary keys or cell contents.
type ColumnDigest struct {
	Table    string `json:"table"`
	Column   string `json:"column"`
	RowCount int64  `json:"row_count"`
	SHA256   string `json:"sha256"`
}

type digestColumn struct {
	name string
	kind digestValueKind
}

// DigestTable reads physical rows directly rather than through GORM's default
// scope, so soft-deleted rows remain part of migration verification. Both the
// selected columns and row order come from the explicit catalog/model contract,
// never from database-specific concatenation or hashing functions.
func DigestTable(ctx context.Context, db *gorm.DB, spec TableSpec) (TableDigest, error) {
	ctx = nonNilContext(ctx)
	if db == nil {
		return TableDigest{}, fmt.Errorf("digest table %q: database is nil", spec.Name)
	}
	if spec.Name == "" || spec.PrimaryKey == "" || spec.Model == nil {
		return TableDigest{}, fmt.Errorf("digest table: incomplete table specification")
	}

	columns, err := digestColumns(db, spec)
	if err != nil {
		return TableDigest{}, err
	}
	quotedColumns := make([]string, len(columns))
	for i, column := range columns {
		quotedColumns[i] = quoteIdentifier(db, column.name)
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s ORDER BY %s ASC",
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(db, spec.Name),
		quoteIdentifier(db, spec.PrimaryKey),
	)

	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return TableDigest{}, fmt.Errorf("digest table %q query: %w", spec.Name, err)
	}
	defer rows.Close()

	tableHash := sha256.New()
	writeDigestFrame(tableHash, []byte(digestFormatVersion))
	writeDigestFrame(tableHash, []byte(spec.Name))
	for _, column := range columns {
		writeDigestFrame(tableHash, []byte(column.name))
		writeDigestFrame(tableHash, []byte{byte(column.kind)})
	}

	var rowCount int64
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return TableDigest{}, fmt.Errorf("digest table %q scan row %d: %w", spec.Name, rowCount+1, err)
		}

		rowHash := sha256.New()
		for i, column := range columns {
			normalized, err := normalizeDigestValue(values[i], column.kind)
			if err != nil {
				return TableDigest{}, fmt.Errorf("digest table %q row %d column %q: %w", spec.Name, rowCount+1, column.name, err)
			}
			writeDigestFrame(rowHash, []byte(column.name))
			writeDigestFrame(rowHash, normalized)
		}
		writeDigestFrame(tableHash, rowHash.Sum(nil))
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return TableDigest{}, fmt.Errorf("digest table %q iterate: %w", spec.Name, err)
	}

	return TableDigest{
		Table:    spec.Name,
		RowCount: rowCount,
		SHA256:   hex.EncodeToString(tableHash.Sum(nil)),
	}, nil
}

// DigestTableColumns computes one credential-free digest per persisted column.
// Rows are framed in primary-key order, matching DigestTable, so callers can
// identify which columns differ without reading or reporting business values.
func DigestTableColumns(ctx context.Context, db *gorm.DB, spec TableSpec) ([]ColumnDigest, error) {
	ctx = nonNilContext(ctx)
	if db == nil {
		return nil, fmt.Errorf("digest table columns %q: database is nil", spec.Name)
	}
	if spec.Name == "" || spec.PrimaryKey == "" || spec.Model == nil {
		return nil, fmt.Errorf("digest table columns: incomplete table specification")
	}

	columns, err := digestColumns(db, spec)
	if err != nil {
		return nil, err
	}
	quotedColumns := make([]string, len(columns))
	columnHashes := make([]hash.Hash, len(columns))
	for i, column := range columns {
		quotedColumns[i] = quoteIdentifier(db, column.name)
		columnHashes[i] = sha256.New()
		writeDigestFrame(columnHashes[i], []byte(columnDigestFormatVersion))
		writeDigestFrame(columnHashes[i], []byte(spec.Name))
		writeDigestFrame(columnHashes[i], []byte(column.name))
		writeDigestFrame(columnHashes[i], []byte{byte(column.kind)})
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s ORDER BY %s ASC",
		strings.Join(quotedColumns, ", "),
		quoteIdentifier(db, spec.Name),
		quoteIdentifier(db, spec.PrimaryKey),
	)
	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("digest table columns %q query: %w", spec.Name, err)
	}
	defer rows.Close()

	var rowCount int64
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("digest table columns %q scan row %d: %w", spec.Name, rowCount+1, err)
		}
		for i, column := range columns {
			normalized, err := normalizeDigestValue(values[i], column.kind)
			if err != nil {
				return nil, fmt.Errorf("digest table columns %q row %d column %q: %w", spec.Name, rowCount+1, column.name, err)
			}
			writeDigestFrame(columnHashes[i], normalized)
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("digest table columns %q iterate: %w", spec.Name, err)
	}

	result := make([]ColumnDigest, len(columns))
	for i, column := range columns {
		result[i] = ColumnDigest{
			Table:    spec.Name,
			Column:   column.name,
			RowCount: rowCount,
			SHA256:   hex.EncodeToString(columnHashes[i].Sum(nil)),
		}
	}
	return result, nil
}

func digestColumns(db *gorm.DB, spec TableSpec) ([]digestColumn, error) {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(spec.Model); err != nil {
		return nil, fmt.Errorf("digest table %q parse model: %w", spec.Name, err)
	}
	if statement.Schema.Table != spec.Name {
		return nil, fmt.Errorf("digest table %q: model resolves to table %q", spec.Name, statement.Schema.Table)
	}

	columns := make([]digestColumn, 0, len(statement.Schema.DBNames))
	primaryKeyFound := false
	for _, name := range statement.Schema.DBNames {
		field := statement.Schema.FieldsByDBName[name]
		if field == nil {
			return nil, fmt.Errorf("digest table %q: model column %q has no schema field", spec.Name, name)
		}
		kind, err := digestKindForField(field)
		if err != nil {
			return nil, fmt.Errorf("digest table %q column %q: %w", spec.Name, name, err)
		}
		columns = append(columns, digestColumn{name: name, kind: kind})
		if name == spec.PrimaryKey {
			primaryKeyFound = true
		}
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("digest table %q: model has no persisted columns", spec.Name)
	}
	if !primaryKeyFound {
		return nil, fmt.Errorf("digest table %q: primary key column %q is not persisted", spec.Name, spec.PrimaryKey)
	}
	return columns, nil
}

func digestKindForField(field *schema.Field) (digestValueKind, error) {
	typeName := strings.ToLower(strings.TrimSpace(field.TagSettings["TYPE"]))
	if field.DataType == schema.DataType("json") || strings.HasPrefix(typeName, "json") {
		return digestValueJSON, nil
	}
	if strings.HasPrefix(typeName, "decimal") || strings.HasPrefix(typeName, "numeric") {
		return digestValueDecimal, nil
	}

	indirectType := field.IndirectFieldType
	if indirectType == reflect.TypeOf(time.Time{}) || indirectType == reflect.TypeOf(gorm.DeletedAt{}) || field.DataType == schema.Time {
		return digestValueTime, nil
	}
	switch field.GORMDataType {
	case schema.String:
		return digestValueString, nil
	case schema.Bytes:
		return digestValueBytes, nil
	case schema.Bool:
		return digestValueBool, nil
	case schema.Int, schema.Uint:
		return digestValueInteger, nil
	case schema.Float:
		return digestValueDecimal, nil
	default:
		return 0, fmt.Errorf("unsupported GORM data type %q (database type %q) for Go type %v", field.GORMDataType, field.DataType, field.FieldType)
	}
}

func normalizeDigestValue(value any, kind digestValueKind) ([]byte, error) {
	if value == nil {
		return []byte{0}, nil
	}
	if deletedAt, ok := value.(gorm.DeletedAt); ok {
		if !deletedAt.Valid {
			return []byte{0}, nil
		}
		value = deletedAt.Time
	}
	if deletedAt, ok := value.(*gorm.DeletedAt); ok {
		if deletedAt == nil || !deletedAt.Valid {
			return []byte{0}, nil
		}
		value = deletedAt.Time
	}

	var normalized string
	var err error
	switch kind {
	case digestValueString:
		normalized, err = digestText(value)
	case digestValueBytes:
		var raw []byte
		raw, err = digestBytes(value)
		if err == nil {
			normalized = base64.StdEncoding.EncodeToString(raw)
		}
	case digestValueBool:
		normalized, err = digestBool(value)
	case digestValueInteger:
		normalized, err = digestInteger(value)
	case digestValueDecimal:
		normalized, err = digestDecimal(value)
	case digestValueTime:
		normalized, err = digestTime(value)
	case digestValueJSON:
		normalized, err = digestJSON(value)
	default:
		err = fmt.Errorf("unsupported digest value kind %d", kind)
	}
	if err != nil {
		return nil, err
	}

	result := make([]byte, 2+len(normalized))
	result[0] = 1
	result[1] = byte(kind)
	copy(result[2:], normalized)
	return result, nil
}

func digestText(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("expected string-compatible value, got %T", value)
	}
}

func digestBytes(value any) ([]byte, error) {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...), nil
	case string:
		return []byte(typed), nil
	default:
		return nil, fmt.Errorf("expected bytes-compatible value, got %T", value)
	}
}

func digestBool(value any) (string, error) {
	switch typed := value.(type) {
	case bool:
		return strconv.FormatBool(typed), nil
	case int64:
		if typed == 0 {
			return "false", nil
		}
		if typed == 1 {
			return "true", nil
		}
	case uint64:
		if typed == 0 {
			return "false", nil
		}
		if typed == 1 {
			return "true", nil
		}
	case []byte:
		return digestBool(string(typed))
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "0", "false":
			return "false", nil
		case "1", "true":
			return "true", nil
		}
	}
	return "", fmt.Errorf("expected boolean value, got %T(%v)", value, value)
}

func digestInteger(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case int:
		return strconv.FormatInt(int64(typed), 10), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case []byte:
		text = string(typed)
	case string:
		text = typed
	default:
		return "", fmt.Errorf("expected integer value, got %T", value)
	}

	integer := new(big.Int)
	if _, ok := integer.SetString(strings.TrimSpace(text), 10); !ok {
		return "", fmt.Errorf("invalid integer %q", text)
	}
	return integer.String(), nil
}

func digestDecimal(value any) (string, error) {
	var text string
	switch typed := value.(type) {
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return "", fmt.Errorf("non-finite decimal %v", typed)
		}
		text = strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", fmt.Errorf("non-finite decimal %v", typed)
		}
		text = strconv.FormatFloat(typed, 'g', -1, 64)
	case int64:
		text = strconv.FormatInt(typed, 10)
	case uint64:
		text = strconv.FormatUint(typed, 10)
	case []byte:
		text = string(typed)
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	default:
		return "", fmt.Errorf("expected decimal value, got %T", value)
	}
	return canonicalDecimal(text)
}

func canonicalDecimal(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", fmt.Errorf("empty decimal")
	}

	negative := false
	switch value[0] {
	case '-':
		negative = true
		value = value[1:]
	case '+':
		value = value[1:]
	}
	if value == "" {
		return "", fmt.Errorf("invalid decimal %q", input)
	}

	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		if strings.ContainsAny(value[index+1:], "eE") {
			return "", fmt.Errorf("invalid decimal %q", input)
		}
		parsed, err := strconv.Atoi(value[index+1:])
		if err != nil || parsed < -10000 || parsed > 10000 {
			return "", fmt.Errorf("invalid decimal exponent in %q", input)
		}
		exponent = parsed
		value = value[:index]
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 || (len(parts) == 1 && parts[0] == "") || (len(parts) == 2 && parts[0] == "" && parts[1] == "") {
		return "", fmt.Errorf("invalid decimal %q", input)
	}
	integerPart := parts[0]
	fractionPart := ""
	if len(parts) == 2 {
		fractionPart = parts[1]
	}
	if integerPart == "" {
		integerPart = "0"
	}
	for _, digit := range integerPart + fractionPart {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("invalid decimal %q", input)
		}
	}

	digits := integerPart + fractionPart
	decimalPosition := len(integerPart) + exponent
	switch {
	case decimalPosition <= 0:
		value = "0." + strings.Repeat("0", -decimalPosition) + digits
	case decimalPosition >= len(digits):
		value = digits + strings.Repeat("0", decimalPosition-len(digits))
	default:
		value = digits[:decimalPosition] + "." + digits[decimalPosition:]
	}

	parts = strings.SplitN(value, ".", 2)
	parts[0] = strings.TrimLeft(parts[0], "0")
	if parts[0] == "" {
		parts[0] = "0"
	}
	if len(parts) == 2 {
		parts[1] = strings.TrimRight(parts[1], "0")
		if parts[1] != "" {
			value = parts[0] + "." + parts[1]
		} else {
			value = parts[0]
		}
	} else {
		value = parts[0]
	}
	if value != "0" && negative {
		value = "-" + value
	}
	return value, nil
}

func digestTime(value any) (string, error) {
	var timestamp time.Time
	switch typed := value.(type) {
	case time.Time:
		timestamp = typed
	case *time.Time:
		if typed == nil {
			return "", fmt.Errorf("nil *time.Time must be represented as SQL NULL")
		}
		timestamp = *typed
	case []byte:
		parsed, err := parseDigestTime(string(typed))
		if err != nil {
			return "", err
		}
		timestamp = parsed
	case string:
		parsed, err := parseDigestTime(typed)
		if err != nil {
			return "", err
		}
		timestamp = parsed
	default:
		return "", fmt.Errorf("expected time value, got %T", value)
	}
	return timestamp.Round(0).UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z"), nil
}

func parseDigestTime(input string) (time.Time, error) {
	value := strings.TrimSpace(input)
	zonedLayouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999 Z07:00",
	}
	for _, layout := range zonedLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	localLayouts := []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range localLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", input)
}

func digestJSON(value any) (string, error) {
	text, err := digestText(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("invalid JSON: multiple values")
		}
		return "", fmt.Errorf("invalid JSON trailing data: %w", err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("canonicalize JSON: %w", err)
	}
	return string(canonical), nil
}

func writeDigestFrame(destination hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = destination.Write(size[:])
	_, _ = destination.Write(value)
}
