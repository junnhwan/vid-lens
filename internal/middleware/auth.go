package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"vid-lens/internal/pkg/jwt"
	"vid-lens/internal/pkg/response"
)

// JWTAuth JWT 认证中间件
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, "请先登录")
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.Unauthorized(c, "Token 格式错误")
			c.Abort()
			return
		}

		claims, err := jwt.ParseToken(parts[1], secret)
		if err != nil {
			response.Unauthorized(c, "Token 无效或已过期")
			c.Abort()
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// GetUserID 从 gin.Context 中获取当前用户 ID
func GetUserID(c *gin.Context) int64 {
	id, exists := c.Get("userID")
	if !exists {
		return 0
	}
	return id.(int64)
}
