package handler

import (
	"github.com/gin-gonic/gin"
	"vid-lens/internal/middleware"
	"vid-lens/internal/pkg/response"
	"vid-lens/internal/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=2,max=50"`
	Password string `json:"password" binding:"required,min=6"`
	Nickname string `json:"nickname"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 用户注册
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	user, token, err := h.svc.Register(req.Username, req.Password, req.Nickname)
	if err != nil {
		if err == service.ErrUserExists {
			response.Fail(c, 400, err.Error())
			return
		}
		response.InternalError(c, "注册失败")
		return
	}

	response.OK(c, gin.H{
		"user":  user,
		"token": token,
	})
}

// Login 用户登录
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	user, token, err := h.svc.Login(req.Username, req.Password)
	if err != nil {
		response.Fail(c, 401, err.Error())
		return
	}

	response.OK(c, gin.H{
		"user":  user,
		"token": token,
	})
}

// GetProfile 获取当前用户信息
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := middleware.GetUserID(c)
	user, err := h.svc.GetUserByID(userID)
	if err != nil {
		response.InternalError(c, "用户不存在")
		return
	}
	response.OK(c, user)
}
