package httpapi

import "net/http"

func (a *Server) runDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	v, err := a.Store.RunDiagnostics(r.Context(), r.PathValue("run"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, v)
}
