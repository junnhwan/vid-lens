package config

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type deprecatedConfigField struct {
	path    []string
	message string
}

var deprecatedConfigFields = []deprecatedConfigField{
	{
		path:    []string{"rag", "collection"},
		message: "配置字段 rag.collection 已弃用，请迁移到 milvus.collection",
	},
	{
		path:    []string{"rag", "rerank_endpoint"},
		message: "配置字段 rag.rerank_endpoint 已删除；legacy 模型 rerank 请使用 cmd/rag-eval --rerank-endpoint",
	},
	{
		path:    []string{"rag", "rerank_model"},
		message: "配置字段 rag.rerank_model 已删除；legacy 模型 rerank 请使用 cmd/rag-eval --rerank-model",
	},
}

// Load parses configuration shape and applies load-time defaults. Commands
// remain responsible for validating the subset of values they actually use.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	expanded := []byte(os.ExpandEnv(string(data)))
	if err := rejectDeprecatedConfigFields(expanded); err != nil {
		return nil, err
	}

	cfg := Config{AIGovernance: defaultAIGovernanceConfig()}
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
	cfg.Kafka.applyDefaults()

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
