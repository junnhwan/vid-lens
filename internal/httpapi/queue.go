package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (a *Server) queue(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	v, err := a.Store.QueueStatus(ctx)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.json(w, http.StatusOK, v)
}
