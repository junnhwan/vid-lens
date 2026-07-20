package main

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/config"
	"vid-lens/internal/handler"
	"vid-lens/internal/middleware"
)

type serverHandlers struct {
	user           *handler.UserHandler
	profiles       *handler.AIProfileHandler
	rag            *handler.RAGHandler
	chat           *handler.ChatHandler
	media          *handler.MediaHandler
	knowledgeBases *handler.KnowledgeBaseHandler
}

// newServerRouter owns HTTP route registration and static SPA fallback. It
// receives already-wired handlers so routing does not know how services are
// constructed or how infrastructure is initialized.
func newServerRouter(cfg config.Config, handlers serverHandlers, rateLimiter *middleware.RateLimiter, readinessChecks []dependencyCheck) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(middleware.CORS())

	api := r.Group("/api/v1")
	{
		api.POST("/user/register", handlers.user.Register)
		api.POST("/user/login", handlers.user.Login)

		auth := api.Group("")
		auth.Use(middleware.JWTAuth(cfg.JWT.Secret))
		{
			auth.GET("/user/profile", handlers.user.GetProfile)
			aiProfiles := auth.Group("/ai/profiles")
			{
				aiProfiles.GET("", handlers.profiles.List)
				aiProfiles.POST("", handlers.profiles.Create)
				aiProfiles.PUT("/:id", handlers.profiles.Update)
				aiProfiles.DELETE("/:id", handlers.profiles.Delete)
				aiProfiles.POST("/test", handlers.profiles.Test)
			}
			chat := auth.Group("/chat")
			{
				chat.POST("/sessions", handlers.chat.CreateSession)
				chat.GET("/sessions", handlers.chat.ListSessions)
				chat.DELETE("/sessions/:session_id", handlers.chat.DeleteSession)
				chat.GET("/sessions/:session_id/messages", handlers.chat.ListMessages)
				chat.POST("/sessions/:session_id/messages", middleware.RateLimit(rateLimiter), handlers.chat.Ask)
				// Experimental: tool-loop agent QA. Not the default product path.
				chat.POST("/sessions/:session_id/messages/agent", middleware.RateLimit(rateLimiter), handlers.chat.AskAgent)
				chat.POST("/sessions/:session_id/messages/stream", middleware.RateLimit(rateLimiter), handlers.chat.AskStream)
			}
			knowledgeBases := auth.Group("/knowledge-bases")
			{
				knowledgeBases.POST("", handlers.knowledgeBases.Create)
				knowledgeBases.GET("", handlers.knowledgeBases.List)
				knowledgeBases.GET("/:id", handlers.knowledgeBases.Get)
				knowledgeBases.PATCH("/:id", handlers.knowledgeBases.Update)
				knowledgeBases.DELETE("/:id", handlers.knowledgeBases.Delete)
				knowledgeBases.POST("/:id/videos", handlers.knowledgeBases.AddVideo)
				knowledgeBases.DELETE("/:id/videos/:task_id", handlers.knowledgeBases.RemoveVideo)
			}
			media := auth.Group("/media")
			{
				media.POST("/upload", handlers.media.UploadFile)
				media.POST("/upload-url", handlers.media.UploadByURL)
				media.POST("/upload-chunk", handlers.media.UploadChunk)
				media.GET("/check-upload", handlers.media.CheckUpload)
				media.POST("/merge-chunks", handlers.media.MergeChunks)
				media.GET("/list", handlers.media.ListTasks)
				media.GET("/task/:id", handlers.media.GetTaskDetail)
				media.DELETE("/task/:id", handlers.media.DeleteTask)
				media.POST("/analyze/:id", middleware.RateLimit(rateLimiter), handlers.media.RequestAnalysis)
				media.POST("/transcribe/:id", middleware.RateLimit(rateLimiter), handlers.media.RequestTranscribe)
				media.GET("/task/:id/rag-index", handlers.rag.GetTaskIndexStatus)
				media.POST("/task/:id/rag-index", middleware.RateLimit(rateLimiter), handlers.rag.BuildTaskIndex)
				media.GET("/download-audio/:id", handlers.media.DownloadAudio)
			}
		}
	}

	r.GET("/health", livenessHandler()) // 保留旧路径兼容已有监控
	r.GET("/healthz", livenessHandler())
	r.GET("/readyz", readinessHandler(readinessChecks, readinessTimeout))
	registerStaticFrontend(r)
	return r
}

func registerStaticFrontend(r *gin.Engine) {
	staticDir := filepath.Join(".", "web", "dist")
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		r.Static("/assets", filepath.Join(staticDir, "assets"))
		r.NoRoute(func(c *gin.Context) {
			// 只处理 GET/HEAD，避免误把 API 404 当页面返回
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
				c.Status(http.StatusNotFound)
				return
			}
			reqPath := path.Clean("/" + c.Request.URL.Path)
			// 禁止路径穿越；仅允许 dist 根下的单层静态文件（favicon 等）
			if reqPath != "/" && !strings.Contains(reqPath[1:], "/") {
				candidate := filepath.Join(staticDir, filepath.Base(reqPath))
				if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
					// 根级静态资源勿被 CDN 长时间缓存错误响应
					c.Header("Cache-Control", "public, max-age=3600")
					c.File(candidate)
					return
				}
			}
			c.File(filepath.Join(staticDir, "index.html"))
		})
		log.Println("✅ 前端静态资源已加载")
	}
}
