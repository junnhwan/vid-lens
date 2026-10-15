package store

import (
	"context"
	"time"
	"vidlens/internal/domain"
)

type Session struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Store) NewSession(ctx context.Context, hash string, ttl time.Duration) (Session, error) {
	var v Session
	e := s.Pool.QueryRow(ctx, "INSERT INTO anonymous_sessions(id,token_hash,expires_at) VALUES($1,$2,now()+$3*interval '1 second') RETURNING id,expires_at", domain.ID(), hash, int64(ttl.Seconds())).Scan(&v.ID, &v.ExpiresAt)
	return v, e
}
func (s *Store) Session(ctx context.Context, hash string) (Session, error) {
	var v Session
	e := s.Pool.QueryRow(ctx, "SELECT id,expires_at FROM anonymous_sessions WHERE token_hash=$1 AND expires_at>now()", hash).Scan(&v.ID, &v.ExpiresAt)
	return v, e
}
