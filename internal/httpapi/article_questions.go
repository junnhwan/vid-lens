package httpapi

import "net/http"

func (a *Server) articleQuestionSuggestions(w http.ResponseWriter, r *http.Request) {
	s, ok := a.auth(w, r)
	if !ok {
		return
	}
	v, err := a.Store.ArticleQuestionSuggestions(r.Context(), s.ID, r.PathValue("request"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, http.StatusOK, v)
}
