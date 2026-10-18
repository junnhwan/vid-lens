package httpapi

import (
	"net/http"
	"vidlens/internal/store"
)

func (a *Server) requestShare(w http.ResponseWriter, r *http.Request) {
	session, ok := a.auth(w, r)
	if !ok {
		return
	}
	var view store.RequestShare
	var err error
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		view, err = a.Store.RequestShare(r.Context(), session.ID, r.PathValue("request"))
	} else {
		view, err = a.Store.SetRequestShare(r.Context(), session.ID, r.PathValue("request"), r.Method == http.MethodPut)
	}
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, http.StatusOK, view)
}

func (a *Server) sharedRecord(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	view, err := a.Store.SharedRecord(r.Context(), r.PathValue("token"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, http.StatusOK, view)
}

func (a *Server) sharedFrame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	asset, err := a.Store.SharedFrame(r.Context(), r.PathValue("token"), r.PathValue("frame"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	// Keep no-store: a revoked share must also lose access to its pictures.
	a.serveStoredAsset(w, r, asset)
}
