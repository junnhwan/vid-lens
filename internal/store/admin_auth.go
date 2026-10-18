package store

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
	"vidlens/internal/domain"
)

const OwnerPasswordMinBytes = 8

func hashPassword(password string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	h := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return "argon2id:v1:" + base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(h)
}
func checkPassword(password, encoded string) bool {
	p := strings.Split(encoded, ":")
	if len(p) != 4 || p[0] != "argon2id" || p[1] != "v1" {
		return false
	}
	s, e := base64.RawStdEncoding.DecodeString(p[2])
	if e != nil || len(s) != 16 {
		return false
	}
	h, e := base64.RawStdEncoding.DecodeString(p[3])
	if e != nil || len(h) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(h, argon2.IDKey([]byte(password), s, 2, 19*1024, 1, 32)) == 1
}
func (s *Store) SetOwnerPassword(ctx context.Context, password string) error {
	if len(password) < OwnerPasswordMinBytes || len(password) > 512 {
		return fmt.Errorf("owner password must contain %d-512 bytes", OwnerPasswordMinBytes)
	}
	h := hashPassword(password)
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "INSERT INTO owner_auth(singleton,password_hash) VALUES(true,$1) ON CONFLICT(singleton) DO UPDATE SET password_hash=$1,auth_version=owner_auth.auth_version+1", h); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "DELETE FROM admin_sessions")
		return e
	})
}
func (s *Store) BeginOwnerLogin(ctx context.Context, source string) (string, int64, error) {
	var hash string
	var version int64
	e := s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "DELETE FROM admin_login_limits WHERE window_start<now()-interval '1 day'"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO admin_login_limits(source_hash,failures,next_attempt_at) VALUES($1,1,now()+interval '1 second') ON CONFLICT(source_hash) DO UPDATE SET failures=CASE WHEN admin_login_limits.window_start<now()-interval '15 minutes' THEN 1 ELSE admin_login_limits.failures+1 END, window_start=CASE WHEN admin_login_limits.window_start<now()-interval '15 minutes' THEN now() ELSE admin_login_limits.window_start END,next_attempt_at=now()+LEAST(30,POWER(2,LEAST(admin_login_limits.failures,5))) * interval '1 second'`, source); e != nil {
			return e
		}
		var n int
		if e := tx.QueryRow(ctx, "SELECT failures FROM admin_login_limits WHERE source_hash=$1", source).Scan(&n); e != nil {
			return e
		}
		if n > 5 {
			return domain.Err(429, "RATE_LIMITED", "登录尝试过多，请十五分钟后重试。")
		}
		return tx.QueryRow(ctx, "SELECT password_hash,auth_version FROM owner_auth").Scan(&hash, &version)
	})
	return hash, version, e
}
func (s *Store) OwnerLoginAllowed(ctx context.Context, source string) error {
	var blocked bool
	e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM admin_login_limits WHERE source_hash=$1 AND window_start>now()-interval '15 minutes' AND (failures>=5 OR next_attempt_at>now()))", source).Scan(&blocked)
	if e != nil {
		return e
	}
	if blocked {
		return domain.Err(429, "RATE_LIMITED", "登录尝试过多，请稍后重试。")
	}
	return nil
}
func (s *Store) VerifyOwnerPassword(password, hash string) bool { return checkPassword(password, hash) }
func (s *Store) NewAdminSession(ctx context.Context, source, token string, version int64) error {
	return s.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var current int64
		if e := tx.QueryRow(ctx, "SELECT auth_version FROM owner_auth").Scan(&current); e != nil {
			return e
		}
		if current != version {
			return domain.Err(401, "SESSION_REQUIRED", "密码已更改，请重新登录。")
		}
		if _, e := tx.Exec(ctx, "DELETE FROM admin_login_limits WHERE source_hash=$1", source); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO admin_sessions(token_hash,auth_version,expires_at,idle_until) VALUES($1,$2,now()+interval '8 hours',now()+interval '30 minutes')", token, version)
		return e
	})
}
func (s *Store) AdminSession(ctx context.Context, token string) (time.Time, error) {
	var expiry time.Time
	e := s.Pool.QueryRow(ctx, "UPDATE admin_sessions s SET idle_until=LEAST(s.expires_at,now()+interval '30 minutes') FROM owner_auth a WHERE s.token_hash=$1 AND s.auth_version=a.auth_version AND s.expires_at>now() AND s.idle_until>now() RETURNING s.expires_at", token).Scan(&expiry)
	return expiry, e
}
func (s *Store) LogoutAdmin(ctx context.Context, token string) error {
	_, e := s.Pool.Exec(ctx, "DELETE FROM admin_sessions WHERE token_hash=$1", token)
	return e
}
