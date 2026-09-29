package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"vid-lens/internal/pkg/remoteurl"
)

// Environment overrides also work with an existing literal production YAML.
// Apply secrets after YAML decoding so quotes, backslashes and '#' are data.
func applyRuntimeEnvironment(c *Config) error {
	stringsByName := map[string]*string{
		"VIDLENS_SERVER_HOST": &c.Server.Host, "VIDLENS_SERVER_MODE": &c.Server.Mode,
		"VIDLENS_DATABASE_HOST": &c.Database.Host, "VIDLENS_DATABASE_USERNAME": &c.Database.Username,
		"VIDLENS_DATABASE_PASSWORD": &c.Database.Password, "VIDLENS_DATABASE_DBNAME": &c.Database.DBName,
		"VIDLENS_DATABASE_SSLMODE": &c.Database.SSLMode,
		"VIDLENS_REDIS_HOST":       &c.Redis.Host, "VIDLENS_REDIS_PASSWORD": &c.Redis.Password,
		"VIDLENS_MINIO_ENDPOINT": &c.MinIO.Endpoint, "VIDLENS_MINIO_ACCESS_KEY": &c.MinIO.AccessKey,
		"VIDLENS_MINIO_SECRET_KEY": &c.MinIO.SecretKey, "VIDLENS_MINIO_BUCKET": &c.MinIO.Bucket,
		"VIDLENS_JWT_SECRET": &c.JWT.Secret, "VIDLENS_API_KEY_SECRET": &c.Security.APIKeySecret,
		"VIDLENS_FFMPEG_PATH": &c.Tools.FFmpegPath, "VIDLENS_YTDLP_PATH": &c.Tools.YtDlpPath,
		"VIDLENS_COOKIES_PATH": &c.Tools.CookiesPath, "VIDLENS_PROXY_URL": &c.Tools.ProxyURL,
		"VIDLENS_OCR_PATH": &c.Tools.OCRPath, "VIDLENS_OCR_LANG": &c.Tools.OCRLang,
	}
	for name, target := range stringsByName {
		if value, exists := os.LookupEnv(name); exists {
			*target = value
		}
	}
	intsByName := map[string]*int{
		"VIDLENS_SERVER_PORT": &c.Server.Port, "VIDLENS_DATABASE_PORT": &c.Database.Port,
		"VIDLENS_REDIS_PORT": &c.Redis.Port, "VIDLENS_REDIS_DB": &c.Redis.DB,
		"VIDLENS_JWT_EXPIRE_HOURS": &c.JWT.ExpireHours,
	}
	for name, target := range intsByName {
		if raw, exists := os.LookupEnv(name); exists {
			value, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s 必须为整数", name)
			}
			*target = value
		}
	}
	if raw, exists := os.LookupEnv("VIDLENS_MINIO_USE_SSL"); exists {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("VIDLENS_MINIO_USE_SSL 必须为布尔值")
		}
		c.MinIO.UseSSL = value
	}
	for name, target := range map[string]*[]string{
		"VIDLENS_MQ_BROKERS":          &c.MQ.Brokers,
		"VIDLENS_ALLOWED_VIDEO_HOSTS": &c.Tools.AllowedVideoHosts,
	} {
		if raw, exists := os.LookupEnv(name); exists {
			var values []string
			for _, value := range strings.Split(raw, ",") {
				value = strings.TrimSpace(value)
				if value == "" {
					return fmt.Errorf("%s 必须为非空的逗号分隔列表", name)
				}
				values = append(values, value)
			}
			*target = values
		}
	}
	return nil
}

func (c *ToolsConfig) applyDefaults() {
	c.FFmpegPath = strings.TrimSpace(c.FFmpegPath)
	if c.FFmpegPath == "" {
		c.FFmpegPath = "ffmpeg"
	}
	c.YtDlpPath = strings.TrimSpace(c.YtDlpPath)
	if c.YtDlpPath == "" {
		c.YtDlpPath = "yt-dlp"
	}
	if len(c.AllowedVideoHosts) == 0 {
		c.AllowedVideoHosts = remoteurl.DefaultAllowedHosts()
		log.Printf("[config] tools.allowed_video_hosts 未配置，使用默认域名: %s", strings.Join(c.AllowedVideoHosts, ", "))
	}
}
