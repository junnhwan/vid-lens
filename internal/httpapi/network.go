package httpapi

import (
	"net/http"
	"vidlens/internal/store"
)

func (a *Server) networkSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var v store.NetworkSettings
	var e error
	if r.Method == "GET" {
		v, e = a.Store.NetworkSettings(r.Context(), a.Config)
	} else {
		var in store.NetworkInput
		if !a.body(w, r, &in) {
			return
		}
		v, e = a.Store.SaveNetworkSettings(r.Context(), a.Config, in)
	}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	usage, e := a.Store.SearchUsage(r.Context())
	used := struct {
		store.NetworkSettings
		store.SearchUsage
	}{v, usage}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, 200, used)
}
