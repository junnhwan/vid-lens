package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
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
	// ==========================================
	// 1. 加载配置
	// ==========================================
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// ==========================================
	// 2. 初始化数据库
	// ==========================================
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	// 自动迁移（开发阶段）
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库连接成功，表结构已同步")

	// ==========================================
	// 3. 初始化 Redis
	// ==========================================
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	log.Println("Redis 连接成功")

	// ==========================================
	// 4. 初始化 MinIO
	// ==========================================
	minioStorage, err := storage.NewMinIOStorage(
		cfg.MinIO.Endpoint,
		cfg.MinIO.AccessKey,
		cfg.MinIO.SecretKey,
		cfg.MinIO.Bucket,
		cfg.MinIO.UseSSL,
	)
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}
	log.Println("MinIO 连接成功")

	// ==========================================
	// 5. 初始化 AI 策略
	// ==========================================
	aiStrategy := ai.NewSiliconFlowStrategy(
		cfg.AI.SiliconFlowAPIKey,
		cfg.AI.SiliconFlowBaseURL,
		cfg.AI.ASRModel,
		cfg.AI.LLMModel,
	)

	// ==========================================
	// 6. 初始化消息队列
	// ==========================================
	repos := repository.NewRepositories(db)

	// Asynq Client（生产者）
	producer, err := mq.NewProducer(cfg.Redis.Addr())
	if err != nil {
		log.Fatalf("初始化消息队列生产者失败: %v", err)
	}

	// Asynq Server（消费者）
	asynqServer := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.Redis.Addr()},
		asynq.Config{
			Concurrency: 4,           // 并发消费者数量
			RetryDelayFunc: asynq.DefaultRetryDelayFunc, // 默认指数退避
			Queues: map[string]int{
				"critical": 6, // 高优先级（AI 分析）
				"default":  3, // 普通优先级（文字提取）
			},
		},
	)

	// 注册消费者处理函数
	worker := mq.NewWorker(repos, minioStorage, aiStrategy, rdb)
	mux := asynq.NewServeMux()
	mux.HandleFunc(mq.TaskTypeAnalyze, worker.HandleAnalyze)
	mux.HandleFunc(mq.TaskTypeTranscribe, worker.HandleTranscribe)

	// 启动消费者（后台运行）
	go func() {
		log.Println("Asynq 消费者已启动...")
		if err := asynqServer.Run(mux); err != nil {
			log.Fatalf("Asynq 消费者异常退出: %v", err)
		}
	}()

	// ==========================================
	// 7. 初始化 Service 层
	// ==========================================
	userSvc := service.NewUserService(repos.User, cfg.JWT)
	mediaSvc := service.NewMediaService(repos, minioStorage, producer, rdb, cfg.Upload)

	// ==========================================
	// 8. 初始化 Handler 层
	// ==========================================
	userHandler := handler.NewUserHandler(userSvc)
	mediaHandler := handler.NewMediaHandler(mediaSvc)

	// ==========================================
	// 9. 初始化限流器
	// ==========================================
	rateLimiter := middleware.NewRateLimiter(rdb, cfg.RateLimit.Capacity, cfg.RateLimit.Rate)

	// ==========================================
	// 10. 启动 HTTP 服务
	// ==========================================
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// 全局中间件
	r.Use(middleware.CORS())

	// API 路由组
	api := r.Group("/api/v1")
	{
		// 公开接口（无需认证）
		api.POST("/user/register", userHandler.Register)
		api.POST("/user/login", userHandler.Login)

		// 需要认证的接口
		auth := api.Group("")
		auth.Use(middleware.JWTAuth(cfg.JWT.Secret))
		{
			// 用户
			auth.GET("/user/profile", userHandler.GetProfile)

			// 媒体 - 基础操作
			media := auth.Group("/media")
			{
				media.POST("/upload", mediaHandler.UploadFile)
				media.POST("/upload-chunk", mediaHandler.UploadChunk)
				media.GET("/check-upload", mediaHandler.CheckUpload)
				media.POST("/merge-chunks", mediaHandler.MergeChunks)
				media.GET("/list", mediaHandler.ListTasks)
				media.GET("/task/:id", mediaHandler.GetTaskDetail)
				media.DELETE("/task/:id", mediaHandler.DeleteTask)

				// AI 分析（加限流保护）
				media.POST("/analyze/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestAnalysis)
				media.POST("/transcribe/:id", middleware.RateLimit(rateLimiter), mediaHandler.RequestTranscribe)

				// 下载
				media.GET("/download-audio/:id", mediaHandler.DownloadAudio)
			}
		}
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("VidLens 服务启动在 http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
