package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MediaStreamPurpose marks a token that only authorizes reading the media
// bytes of one task. It is deliberately distinct from a session token so a
// stream URL leaked into logs or history cannot be replayed as an authenticated
// API client.
const MediaStreamPurpose = "media-stream"

// MediaTokenTTL is long enough to cover a full viewing session. Stream requests
// carry no refresh path, so a shorter window would surface as a stalled player.
const MediaTokenTTL = 6 * time.Hour

// MediaClaims authorizes a single browser <video> or <img> request. Those
// elements cannot attach an Authorization header, so the credential travels in
// the query string and must be scoped to exactly one task.
type MediaClaims struct {
	UserID  int64  `json:"user_id"`
	TaskID  int64  `json:"task_id"`
	Purpose string `json:"pur"`
	jwt.RegisteredClaims
}

// GenerateMediaToken issues a task-scoped read credential.
func GenerateMediaToken(userID, taskID int64, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := MediaClaims{
		UserID:  userID,
		TaskID:  taskID,
		Purpose: MediaStreamPurpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "vidlens",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseMediaToken validates a stream credential and rejects any other token
// type, including valid session tokens.
func ParseMediaToken(tokenString, secret string) (*MediaClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&MediaClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("vidlens"),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*MediaClaims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的媒体令牌")
	}
	if claims.Purpose != MediaStreamPurpose {
		return nil, errors.New("媒体令牌用途不符")
	}
	return claims, nil
}
