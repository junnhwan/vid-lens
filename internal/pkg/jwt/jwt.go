package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionPurpose marks a token that authenticates API requests.
const SessionPurpose = "session"

// Claims JWT 载荷
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// Purpose distinguishes token types signed with the same secret, so a
	// media-stream credential cannot be replayed as a session token. Session
	// tokens issued before this field existed leave it empty; the parser only
	// rejects a non-empty foreign purpose so adding the field does not log
	// existing users out.
	Purpose string `json:"pur,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT Token
func GenerateToken(userID int64, username, role, secret string, expireHours int) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		Purpose:  SessionPurpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "vidlens",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析 JWT Token
func ParseToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("vidlens"),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的 token")
	}
	if claims.Purpose != "" && claims.Purpose != SessionPurpose {
		return nil, errors.New("token 用途不符")
	}

	return claims, nil
}
