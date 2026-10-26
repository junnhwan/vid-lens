package httpapi

import (
	"net/http"
	"time"

	"vidlens/internal/store"
)

func (a *Server) articleDraft(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, err := a.Store.RequestDraft(r.Context(), s.ID, r.PathValue("request"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, v)
}

func (a *Server) articleReviews(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	request := r.PathValue("request")
	if r.Method == "POST" {
		key, ok := a.key(w, r)
		if !ok {
			return
		}
		if !a.body(w, r, &struct{}{}) {
			return
		}
		accepted, err := a.Store.CreateArticleReview(a.submissionContext(r), s.ID, request, key)
		if err != nil {
			a.problem(w, r, err)
			return
		}
		v, err := a.Store.ArticleReview(r.Context(), accepted.ID)
		if err != nil {
			a.problem(w, r, err)
			return
		}
		w.Header().Set("Idempotency-Replayed", fmtBool(accepted.Replayed))
		w.Header().Set("Idempotency-Expires-At", accepted.Expires.Format(time.RFC3339))
		a.json(w, 202, store.ArticleReviewView(&v))
		return
	}
	v, err := a.Store.LatestArticleReview(r.Context(), s.ID, request)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, store.ArticleReviewView(v))
}
