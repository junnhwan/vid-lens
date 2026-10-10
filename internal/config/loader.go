package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type deprecatedConfigField struct {
	path    []string
	message string
}

var deprecatedConfigFields = []deprecatedConfigField{
	{
		path:    []string{"rag", "collection"},
		message: "配置字段 rag.collection 已删除；pgvector 使用 rag.vector_table",
	},
	{
		path:    []string{"rag", "rerank_endpoint"},
		message: "配置字段 rag.rerank_endpoint 已删除；legacy 模型 rerank 请使用 cmd/rag-eval --rerank-endpoint",
	},
}

// Load parses configuration shape and applies load-time defaults. Commands
// remain responsible for validating the subset of values they actually use.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := loadDotEnv(path); err != nil {
		return nil, err
	}
	expanded := expandConfigEnvironment(data)
	if err := rejectDeprecatedConfigFields(expanded); err != nil {
		return nil, err
	}

	cfg := Config{
		AgentBudget:  DefaultAgentBudgetConfig(),
		AIGovernance: defaultAIGovernanceConfig(),
		MQ: MQConfig{
			ASRConcurrency: DefaultASRConcurrency, ASRMaxRetries: DefaultASRMaxRetries,
			ASRRetryBackoffMS: []int{1000, 3000},
		},
	}
	decoder := yaml.NewDecoder(bytes.NewReader(expanded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	var trailingDocument yaml.Node
	if err := decoder.Decode(&trailingDocument); err == nil {
		return nil, fmt.Errorf("解析配置文件失败: 配置文件不能包含多个 YAML 文档")
	} else if err != io.EOF {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if err := cfg.AgentBudget.Validate(); err != nil {
		return nil, err
	}
	if err := applyRuntimeEnvironment(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.SummaryExperience.applyEnvironment(); err != nil {
		return nil, err
	}
	cfg.Tools.applyDefaults()
	cfg.MQ.applyDefaults()
	cfg.Memory.applyDefaults()
	if value := strings.TrimSpace(os.Getenv("VIDLENS_DISABLE_URL_IMPORT")); value != "" {
		disabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("VIDLENS_DISABLE_URL_IMPORT 必须为布尔值")
		}
		cfg.Upload.DisableURLImport = disabled
	}
	if value := strings.TrimSpace(os.Getenv("VIDLENS_UPLOAD_SLOW_NOTICE")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("VIDLENS_UPLOAD_SLOW_NOTICE 必须为布尔值")
		}
		cfg.Upload.SlowUploadNotice = enabled
	}
	if value := strings.TrimSpace(os.Getenv("VIDLENS_HOSTED_AI_OWNER_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("VIDLENS_HOSTED_AI_OWNER_ID 必须为正整数")
		}
		cfg.Security.HostedAIOwnerID = id
	}
	if cfg.Security.HostedAIOwnerID < 0 {
		return nil, fmt.Errorf("security.hosted_ai_owner_id 不能为负数")
	}

	if err := applyAIGovernanceEnv(&cfg.AIGovernance); err != nil {
		return nil, fmt.Errorf("AI 治理配置无效: %w", err)
	}

	return &cfg, nil
}

func rejectDeprecatedConfigFields(data []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	for _, field := range deprecatedConfigFields {
		if yamlMappingHasPath(&document, field.path) {
			return fmt.Errorf("解析配置文件失败: %s", field.message)
		}
	}
	return nil
}

func yamlMappingHasPath(node *yaml.Node, path []string) bool {
	if node == nil || len(path) == 0 {
		return false
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return false
		}
		return yamlMappingHasPath(node.Content[0], path)
	}
	if node.Kind != yaml.MappingNode {
		return false
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Value != path[0] {
			continue
		}
		if len(path) == 1 {
			return true
		}
		return yamlMappingHasPath(value, path[1:])
	}
	return false
}
