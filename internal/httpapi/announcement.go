package httpapi

import (
	"net/http"

	"vidlens/internal/store"
)

func (a *Server) publicAnnouncement(w http.ResponseWriter, r *http.Request) {
	feed, e := a.Store.PublicAnnouncements(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, http.StatusOK, feed)
}

func (a *Server) announcementSettings(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var list store.AnnouncementList
	var e error
	if r.Method == http.MethodPut {
		var in store.AnnouncementCommand
		if !a.body(w, r, &in) {
			return
		}
		list, e = a.Store.ApplyAnnouncement(r.Context(), in)
	} else {
		list, e = a.Store.Announcements(r.Context())
	}
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, http.StatusOK, list)
}
