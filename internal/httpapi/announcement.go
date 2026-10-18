package httpapi

import (
	"net/http"

	"vidlens/internal/store"
)

func (a *Server) publicAnnouncement(w http.ResponseWriter, r *http.Request) {
	v, e := a.Store.Announcement(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	var published any
	if v.Enabled {
		published = map[string]string{"version": v.Version, "title": v.Title, "body": v.Body}
	}
	a.json(w, http.StatusOK, map[string]any{"announcement": published})
}

func (a *Server) announcementSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var v store.Announcement
	var e error
	if r.Method == http.MethodPut {
		var in store.AnnouncementInput
		if !a.body(w, r, &in) {
			return
		}
		v, e = a.Store.SaveAnnouncement(r.Context(), in)
	} else {
		v, e = a.Store.Announcement(r.Context())
	}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, http.StatusOK, v)
}
