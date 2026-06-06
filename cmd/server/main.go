package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"vid-lens/internal/ai"
	"vid-lens/internal/config"
	"vid-lens/internal/handler"
	"vid-lens/internal/middleware"
	"vid-lens/internal/model"
	"vid-lens/internal/mq"
	"vid-lens/internal/repository"
	"vid-lens/internal/service"
	"vid-lens/internal/storage"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 数据库
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	if err := model.Migrate(db); err != nil {
		log.Fatalf("迁移数据库失败: %v", err)
	}
	log.Println("✅ 数据库连接成功")

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	log.Println("✅ Redis 连接成功")

	// MinIO
	minioStorage, err := storage.NewMinIOStorage(
		cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket, cfg.MinIO.UseSSL,
	)
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}
	log.Println("✅ MinIO 连接成功")

	// AI
	var aiStrategy ai.Strategy
	switch strings.ToLower(cfg.AI.Provider) {
	case "", "siliconflow":
		aiStrategy = ai.NewSiliconFlowStrategy(
			cfg.AI.SiliconFlowAPIKey, cfg.AI.SiliconFlowBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	case "mimo":
		aiStrategy = ai.NewMimoStrategy(
			cfg.AI.MimoAPIKey, cfg.AI.MimoBaseURL,
			cfg.AI.ASRModel, cfg.AI.LLMModel,
		)
	default:
		log.Fatalf("不支持的 AI provider: %s", cfg.AI.Provider)
	}

	// Kafka
	mq.CreateTopics(cfg.Kafka.Brokers, []string{
		cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic,
	})
	producer := mq.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.AnalyzeTopic, cfg.Kafka.TranscribeTopic)
	defer producer.Close()
	log.Println("✅ Kafka 生产者就绪")

	repos := repository.NewRepositories(db)
	consumer := mq.NewConsumer(repos, minioStorage, aiStrategy, rdb, cfg.Tools.FFmpegPath)
	consumer.StartAnalyzeConsumer(cfg.Kafka.Brokers, cfg.Kafka.AnalyzeTopic, cfg.Kafka.ConsumerGroup)
	consumer.StartTranscribeConsumer(cfg.Kafka.Brokers, cfg.Kafka.TranscribeTopic, cfg.Kafka.ConsumerGroup)

	// Service & Handler
	userSvc := service.NewUserService(repos.User, cfg.JWT)
	mediaSvc := service.NewMediaService(repos, minioStorage, producer, rdb, cfg.Upload, cfg.Tools)
	userHandler := handler.NewUserHandler(userSvc)
	mediaHandler := handler.NewMediaHandler(mediaSvc)
	rateLimiter := middleware.NewRateLimiter(rdb, cfg.RateLimit.Capacity, cfg.RateLimit.Rate)

	// HTTP
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(middleware.CORS())

	api := r.Group("/api/v1")
	{
		api.POST("/user/register", userHandler.Register)
		api.POST("/user/login", userHandler.Login)

		auth := api.Group("")
		auth.Use(middleware.JWTAuth(cfg.JWT.Secret))
		{
			auth.GET("/user/profile", userHandler.GetProfile)
			media := auth.Group("/media")
			{
				media.POST("/upload", mediaHandler.UploadFile)
				media.POST("/upload-url", mediaHandler.UploadByURL)
				media.POST("/upload-chunk", mediaHandler.UploadChunk)
				media.GET("/check-upload", mediaHandler.CheckUpload)
				media.POST("/merge-chunks", mediaHandler.MergeChunks)
				media.GET("/list", mediaHandler.ListTasks)
				media.GET("/task/:id", mediaHandler.GetTaskDetail)
				media.DELETE("/task/:id", mediaHandler.DeleteTask)
				media.POST("/analyze/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestAnalysis)
				media.POST("/transcribe/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestTranscribe)
				media.GET("/download-audio/:id", mediaHandler.DownloadAudio)
			}
		}
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "VidLens"})
	})

	// 前端静态文件
	staticDir := filepath.Join(".", "web", "dist")
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		r.Static("/assets", filepath.Join(staticDir, "assets"))
		r.NoRoute(func(c *gin.Context) { c.File(filepath.Join(staticDir, "index.html")) })
		log.Println("✅ 前端静态资源已加载")
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("🚀 VidLens 服务启动在 http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
