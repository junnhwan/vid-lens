package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsMissingMediaTools(t *testing.T) {
	for _, raw := range []string{"", "tools:\n  ffmpeg_path: '  '\n  ytdlp_path: ''\n"} {
		cfg, err := Load(writeLoaderTestConfig(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.FFmpegPath != "ffmpeg" || cfg.Tools.YtDlpPath != "yt-dlp" {
			t.Fatalf("missing PATH defaults: %#v", cfg.Tools)
		}
	}
}

func TestLoadRuntimeEnvironmentOverridesLiteralConfiguration(t *testing.T) {
	t.Setenv("VIDLENS_DATABASE_HOST", "postgres.internal")
	t.Setenv("VIDLENS_MINIO_ENDPOINT", "minio.internal:9000")
	t.Setenv("VIDLENS_DATABASE_PASSWORD", "quoted\"#password\\with:symbols")
	t.Setenv("VIDLENS_MQ_BROKERS", "mq-a:5672,mq-b:5672")
	t.Setenv("VIDLENS_MINIO_USE_SSL", "true")
	t.Setenv("VIDLENS_JWT_SECRET", "environment-jwt-secret")
	cfg, err := Load(writeLoaderTestConfig(t, "database:\n  host: 127.0.0.1\nminio:\n  endpoint: 127.0.0.1:9000\njwt:\n  secret: legacy\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "postgres.internal" || cfg.MinIO.Endpoint != "minio.internal:9000" || cfg.Database.Password != "quoted\"#password\\with:symbols" || !cfg.MinIO.UseSSL || strings.Join(cfg.MQ.Brokers, ",") != "mq-a:5672,mq-b:5672" || cfg.JWT.Secret != "environment-jwt-secret" {
		t.Fatal("runtime environment failed to override literal configuration")
	}
}

func TestValidateServerRejectsProductionDefaultSecrets(t *testing.T) {
	cfg := validServerConfig()
	cfg.Server.Mode = "release"
	cfg.JWT.Secret = "vidlens-jwt-secret-change-in-production"
	err := cfg.ValidateServer()
	if err == nil || !strings.Contains(err.Error(), "jwt.secret") || !strings.Contains(err.Error(), "security.api_key_secret") {
		t.Fatalf("unsafe production secrets accepted: %v", err)
	}
}

func TestValidateServerRequiresIndependentStableProductionSecrets(t *testing.T) {
	cfg := validServerConfig()
	cfg.Server.Mode = "release"
	cfg.JWT.Secret = strings.Repeat("a", 64)
	cfg.Security.APIKeySecret = strings.Repeat("b", 64)
	if err := cfg.ValidateServer(); err != nil {
		t.Fatal(err)
	}
	cfg.Security.APIKeySecret = cfg.JWT.Secret
	if err := cfg.ValidateServer(); err == nil {
		t.Fatal("reused signing/encryption secret accepted")
	}
}

func TestValidateServerRejectsUnderscorePlaceholderSecret(t *testing.T) {
	cfg := validServerConfig()
	cfg.Server.Mode = "release"
	cfg.JWT.Secret = "VIDLENS_JWT_SECRET_CHANGE_ME_IN_PRODUCTION"
	cfg.Security.APIKeySecret = strings.Repeat("b", 64)
	if err := cfg.ValidateServer(); err == nil {
		t.Fatal("underscore variant of production example secret accepted")
	}
}

func TestValidateServerRejectsChangeThisPlaceholderSecret(t *testing.T) {
	cfg := validServerConfig()
	cfg.Server.Mode = "release"
	cfg.JWT.Secret = "vidlens-server-jwt-change-this-secret"
	cfg.Security.APIKeySecret = strings.Repeat("b", 64)
	if err := cfg.ValidateServer(); err == nil {
		t.Fatal("change-this production placeholder accepted")
	}
}

func TestLoadRejectsInvalidRuntimeEnvironmentWithoutEchoingValues(t *testing.T) {
	for _, name := range []string{"VIDLENS_DATABASE_PORT", "VIDLENS_MINIO_USE_SSL", "VIDLENS_MQ_BROKERS", "VIDLENS_ALLOWED_VIDEO_HOSTS"} {
		t.Run(name, func(t *testing.T) {
			value := "not-valid-secret-value"
			if strings.HasSuffix(name, "BROKERS") || strings.HasSuffix(name, "HOSTS") {
				value += ","
			}
			t.Setenv(name, value)
			_, err := Load(writeLoaderTestConfig(t, ""))
			if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), value) {
				t.Fatalf("invalid environment error: %v", err)
			}
		})
	}
}
