package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 全局配置结构体
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	MinIO     MinIOConfig     `yaml:"minio"`
	Kafka     KafkaConfig     `yaml:"kafka"`
	AI        AIConfig        `yaml:"ai"`
	Tools     ToolsConfig     `yaml:"tools"`
	JWT       JWTConfig       `yaml:"jwt"`
	Upload    UploadConfig    `yaml:"upload"`
	RateLimit RateLimitConfig `yaml:"ratelimit"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	Charset  string `yaml:"charset"`
}

func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		d.Username, d.Password, d.Host, d.Port, d.DBName, d.Charset)
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type MinIOConfig struct {
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
}

type KafkaConfig struct {
	Brokers         []string `yaml:"brokers"`
	AnalyzeTopic    string   `yaml:"analyze_topic"`
	TranscribeTopic string   `yaml:"transcribe_topic"`
	ConsumerGroup   string   `yaml:"consumer_group"`
}

type AIConfig struct {
	Provider           string `yaml:"provider"`
	SiliconFlowAPIKey  string `yaml:"siliconflow_api_key"`
	SiliconFlowBaseURL string `yaml:"siliconflow_base_url"`
	MimoAPIKey         string `yaml:"mimo_api_key"`
	MimoBaseURL        string `yaml:"mimo_base_url"`
	ASRModel           string `yaml:"asr_model"`
	LLMModel           string `yaml:"llm_model"`
}

type ToolsConfig struct {
	FFmpegPath string `yaml:"ffmpeg_path"`
	YtDlpPath  string `yaml:"ytdlp_path"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type UploadConfig struct {
	MaxFileSize int64 `yaml:"max_file_size"`
	ChunkSize   int64 `yaml:"chunk_size"`
}

type RateLimitConfig struct {
	Capacity int `yaml:"capacity"`
	Rate     int `yaml:"rate"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &cfg, nil
}
