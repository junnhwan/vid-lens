package config

import (
	"errors"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Profile is frozen for each accepted run. Host admission can impose lower current limits.
type Profile struct {
	ModelProxyPolicy     string   `json:"model_proxy_policy,omitempty"`
	CachePolicy          string   `json:"cache_policy,omitempty"`
	CacheHintsVerified   bool     `json:"cache_hints_verified,omitempty"`
	ModelDisableThinking bool     `json:"model_disable_thinking"`
	ModelBaseURL         string   `json:"model_base_url"`
	ModelName            string   `json:"model_name"`
	ModelKeyRef          string   `json:"model_key_ref"`
	ASRBaseURL           string   `json:"asr_base_url"`
	ASRModel             string   `json:"asr_model"`
	ASRKeyRef            string   `json:"asr_key_ref"`
	MaxVideoMS           int64    `json:"max_video_ms"`
	MaxDownloadBytes     int64    `json:"max_download_bytes"`
	WorkspaceBytes       int64    `json:"workspace_bytes"`
	GenerationSeconds    int64    `json:"generation_seconds"`
	PrepareActiveSeconds int64    `json:"prepare_active_seconds"`
	ArticleActiveSeconds int64    `json:"article_active_seconds"`
	RestoreActiveSeconds int64    `json:"restore_active_seconds"`
	AnswerActiveSeconds  int64    `json:"answer_active_seconds"`
	AnswerSeconds        int64    `json:"answer_seconds"`
	RestoreAnswerSeconds int64    `json:"restore_answer_seconds"`
	QueueSeconds         int64    `json:"queue_seconds"`
	AnswerQueueSeconds   int64    `json:"answer_queue_seconds"`
	IdempotencySeconds   int64    `json:"idempotency_seconds"`
	QuestionCharsMax     int      `json:"question_chars_max"`
	SelectionCharsMax    int      `json:"selection_chars_max"`
	PromptVersion        string   `json:"prompt_version"`
	MaxIterations        int      `json:"max_iterations"`
	OverviewBins         int      `json:"overview_bins"`
	MaxOutputTokens      int      `json:"max_output_tokens"`
	MaxInputChars        int      `json:"max_input_chars"`
	InputCharsLimit      int      `json:"input_chars_limit,omitempty"`
	ContextTokens        int      `json:"context_tokens,omitempty"`
	Platforms            []string `json:"platforms"`
}

func DefaultProfile() Profile {
	return Profile{ModelKeyRef: "model", ASRKeyRef: "asr", MaxVideoMS: 90 * 60 * 1000, MaxDownloadBytes: 2 << 30, WorkspaceBytes: 3 << 30, GenerationSeconds: 7200, PrepareActiveSeconds: 2700, ArticleActiveSeconds: 2700, RestoreActiveSeconds: 1200, AnswerActiveSeconds: 300, AnswerSeconds: 600, RestoreAnswerSeconds: 1800, QueueSeconds: 1200, AnswerQueueSeconds: 120, IdempotencySeconds: 86400, QuestionCharsMax: 4000, SelectionCharsMax: 1000, PromptVersion: "vidlens-evidence-v1", MaxIterations: 48, OverviewBins: 12, MaxOutputTokens: 10000, MaxInputChars: 180000, Platforms: []string{"bilibili", "youtube"}}
}
func (p Profile) ActiveBudget(kind string) time.Duration {
	s := p.ArticleActiveSeconds
	switch kind {
	case "source.prepare":
		s = p.PrepareActiveSeconds
	case "source.restore":
		s = p.RestoreActiveSeconds
	case "qa.answer":
		s = p.AnswerActiveSeconds
	}
	return time.Duration(s) * time.Second
}
func (p Profile) MaxRetries(kind string) int {
	if kind == "qa.answer" || kind == "source.restore" {
		return 1
	}
	return 2
}
func (p Profile) Validate() error {
	if p.InputCharsLimit < 0 || p.InputCharsLimit > 8000000 || p.ContextTokens < 0 || p.ContextTokens > 4000000 {
		return errors.New("invalid separate input/context limits")
	}
	u, e := url.Parse(p.ModelBaseURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.TrimSpace(p.ModelName) == "" {
		return errors.New("model endpoint and model name are required")
	}
	if u.User != nil {
		return errors.New("endpoint credentials must use the credential store")
	}
	if p.MaxVideoMS <= 0 || p.MaxVideoMS > 24*3600*1000 || p.MaxDownloadBytes <= 0 || p.WorkspaceBytes < p.MaxDownloadBytes || p.MaxIterations < 8 || p.MaxIterations > 100 || p.OverviewBins < 1 || p.OverviewBins > 32 || p.MaxOutputTokens < 1024 || p.MaxInputChars < 10000 || p.QuestionCharsMax < 1 || p.SelectionCharsMax < 1 || p.GenerationSeconds < 60 || p.AnswerSeconds < 30 || p.IdempotencySeconds < 60 || p.RestoreAnswerSeconds < p.AnswerSeconds || p.PrepareActiveSeconds <= 0 || p.ArticleActiveSeconds <= 0 || p.AnswerActiveSeconds <= 0 || p.RestoreActiveSeconds <= 0 || p.QueueSeconds <= 0 || p.AnswerQueueSeconds <= 0 {
		return errors.New("invalid execution limits")
	}
	for _, v := range p.Platforms {
		if v != "bilibili" && v != "youtube" {
			return errors.New("unsupported platform")
		}
	}
	return nil
}

type Config struct {
	DatabaseURL, RedisURL, DataDir, Listen, Origin, AdminToken, SecretKey, DevMediaDir, CookieFile, ProxyURL string
	SecureCookie                                                                                             bool
	SessionTTL                                                                                               time.Duration
	Profile                                                                                                  Profile
	ModelKey, ASRKey                                                                                         string
	ModelAllowedHosts                                                                                        []string
	ReferenceReadEnabled                                                                                     bool
	SearchEnabled                                                                                            bool
	TavilyAPIKey                                                                                             string
	SearchMonthlyLimit                                                                                       int
	VideoConcurrency                                                                                         int
}

// MaxVideoConcurrency bounds the worker pool, not the recommended host capacity.
const MaxVideoConcurrency = 64

// VideoSlots supplies the initial value until the owner saves runtime settings.
func (c Config) VideoSlots() int { return max(1, c.VideoConcurrency) }

func Load() (Config, error) { return LoadFile("") }
func LoadFile(path string) (Config, error) {
	values := map[string]string{}
	if path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return Config{}, e
		}
		if e = yaml.Unmarshal(b, &values); e != nil {
			return Config{}, errors.New("invalid YAML configuration")
		}
	}
	get := func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		return values[strings.ToLower(strings.TrimPrefix(k, "VIDLENS_"))]
	}
	env := func(k, d string) string {
		if v := get(k); v != "" {
			return v
		}
		return d
	}
	p := DefaultProfile()
	p.ModelBaseURL = get("VIDLENS_MODEL_BASE_URL")
	p.ModelName = get("VIDLENS_MODEL_NAME")
	p.ModelDisableThinking = get("VIDLENS_MODEL_DISABLE_THINKING") == "true"
	p.ASRBaseURL = get("VIDLENS_ASR_BASE_URL")
	p.ASRModel = get("VIDLENS_ASR_MODEL")
	c := Config{DatabaseURL: get("VIDLENS_DATABASE_URL"), RedisURL: env("VIDLENS_REDIS_URL", "redis://127.0.0.1:6379/0"), DataDir: get("VIDLENS_DATA_DIR"), Listen: env("VIDLENS_LISTEN", "127.0.0.1:8080"), Origin: env("VIDLENS_PUBLIC_ORIGIN", "http://127.0.0.1:8080"), AdminToken: get("VIDLENS_ADMIN_TOKEN"), SecretKey: get("VIDLENS_SECRET_KEY"), DevMediaDir: get("VIDLENS_DEV_MEDIA_DIR"), CookieFile: get("VIDLENS_PLATFORM_COOKIE_FILE"), ProxyURL: get("VIDLENS_PROXY_URL"), Profile: p, ModelKey: get("VIDLENS_MODEL_API_KEY"), ASRKey: get("VIDLENS_ASR_API_KEY"), SessionTTL: 30 * 24 * time.Hour}
	c.SecureCookie = strings.HasPrefix(c.Origin, "https://")
	if v := get("VIDLENS_VIDEO_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > MaxVideoConcurrency {
			return c, errors.New("video concurrency must be 1-64")
		}
		c.VideoConcurrency = n
	}
	if v := get("VIDLENS_MODEL_ALLOWED_HOSTS"); v != "" {
		c.ModelAllowedHosts = strings.Split(v, ",")
	}
	c.ReferenceReadEnabled = get("VIDLENS_REFERENCE_READ_ENABLED") == "true"
	c.SearchEnabled = get("VIDLENS_SEARCH_ENABLED") == "true"
	c.TavilyAPIKey = get("VIDLENS_TAVILY_API_KEY")
	c.SearchMonthlyLimit = 1000
	if v := get("VIDLENS_SEARCH_MONTHLY_LIMIT"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 || n > 1000 {
			return c, errors.New("search monthly limit must be 0-1000")
		}
		c.SearchMonthlyLimit = n
	}
	if c.DatabaseURL == "" || c.DataDir == "" || len(c.AdminToken) < 32 || len(c.SecretKey) < 32 || strings.HasPrefix(c.AdminToken, "REPLACE_") || strings.HasPrefix(c.SecretKey, "REPLACE_") {
		return c, errors.New("database URL, external data directory, admin token and secret key (each at least 32 characters) are required")
	}
	abs, e := filepath.Abs(c.DataDir)
	if e != nil {
		return c, e
	}
	c.DataDir = abs
	wd, _ := os.Getwd()
	if abs == wd || strings.HasPrefix(abs, wd+string(os.PathSeparator)) {
		return c, errors.New("runtime data must be outside the repository")
	}
	if c.DevMediaDir != "" {
		c.DevMediaDir, e = filepath.Abs(c.DevMediaDir)
		if e != nil {
			return c, e
		}
	}
	return c, nil
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
