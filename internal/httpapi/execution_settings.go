package httpapi

import (
	"net/http"
	"vidlens/internal/store"
)

func (a *Server) executionSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var v store.ExecutionSettings
	var e error
	if r.Method == "PUT" {
		var in store.ExecutionInput
		if !a.body(w, r, &in) {
			return
		}
		v, e = a.Store.SaveExecutionSettings(r.Context(), in)
	} else {
		v, e = a.Store.ExecutionSettings(r.Context())
	}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.json(w, 200, v)
}
