package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const maxDotEnvLineSize = 1024 * 1024

// loadDotEnv loads the .env file next to the selected YAML configuration.
// Existing process environment variables deliberately win over .env values.
// The values are installed in the process environment so code paths that use
// os.Getenv directly observe the same local configuration as YAML expansion.
func loadDotEnv(configPath string) error {
	dotEnvPath := filepath.Join(filepath.Dir(configPath), ".env")
	file, err := os.Open(dotEnvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 .env 失败: %w", err)
	}
	defer file.Close()

	values, err := parseDotEnv(file)
	if err != nil {
		return fmt.Errorf("解析 .env 失败: %w", err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("设置 .env 变量 %s 失败: %w", key, err)
		}
	}
	return nil
}

func parseDotEnv(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxDotEnvLineSize)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export") && len(line) > len("export") && unicode.IsSpace(rune(line[len("export")])) {
			line = strings.TrimSpace(line[len("export"):])
		}

		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("第 %d 行缺少 '='", lineNumber)
		}
		key = strings.TrimSpace(key)
		if !validDotEnvKey(key) {
			return nil, fmt.Errorf("第 %d 行变量名无效: %q", lineNumber, key)
		}
		value, err := parseDotEnvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("第 %d 行变量 %s: %w", lineNumber, key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func validDotEnvKey(key string) bool {
	if key == "" || !(key[0] == '_' || key[0] >= 'A' && key[0] <= 'Z' || key[0] >= 'a' && key[0] <= 'z') {
		return false
	}
	for i := 1; i < len(key); i++ {
		if key[i] == '_' || key[i] >= 'A' && key[i] <= 'Z' || key[i] >= 'a' && key[i] <= 'z' || key[i] >= '0' && key[i] <= '9' {
			continue
		}
		return false
	}
	return true
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	switch raw[0] {
	case '\'':
		return parseQuotedDotEnvValue(raw, '\'')
	case '"':
		return parseQuotedDotEnvValue(raw, '"')
	default:
		return stripDotEnvInlineComment(raw), nil
	}
}

func parseQuotedDotEnvValue(raw string, quote byte) (string, error) {
	escaped := false
	closing := -1
	for i := 1; i < len(raw); i++ {
		if quote == '"' && raw[i] == '\\' && !escaped {
			escaped = true
			continue
		}
		if raw[i] == quote && !escaped {
			closing = i
			break
		}
		escaped = false
	}
	if closing == -1 {
		return "", fmt.Errorf("引号未闭合")
	}
	if trailing := strings.TrimSpace(raw[closing+1:]); trailing != "" && !strings.HasPrefix(trailing, "#") {
		return "", fmt.Errorf("引号后存在无效内容")
	}
	if quote == '\'' {
		return raw[1:closing], nil
	}
	value, err := strconv.Unquote(raw[:closing+1])
	if err != nil {
		return "", fmt.Errorf("双引号转义无效: %w", err)
	}
	return value, nil
}

func stripDotEnvInlineComment(raw string) string {
	for i, r := range raw {
		if r == '#' && (i == 0 || unicode.IsSpace(rune(raw[i-1]))) {
			return strings.TrimSpace(raw[:i])
		}
	}
	return strings.TrimSpace(raw)
}

func expandConfigEnvironment(data []byte) []byte {
	return []byte(os.Expand(string(data), func(expression string) string {
		if key, fallback, ok := strings.Cut(expression, ":-"); ok {
			if value, exists := os.LookupEnv(key); exists && value != "" {
				return value
			}
			return fallback
		}
		return os.Getenv(expression)
	}))
}
