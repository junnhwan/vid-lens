package httpapi

import "net/http"

func (a *Server) conversations(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	limit, err := page(r)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	result, err := a.Store.Conversations(r.Context(), ses.ID, r.PathValue("request"), r.URL.Query().Get("before"), limit)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	a.json(w, 200, result)
}

func (a *Server) newConversation(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	key, ok := a.key(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	accepted, err := a.Store.CreateConversation(r.Context(), ses.ID, r.PathValue("request"), key)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	c, err := a.Store.Conversation(r.Context(), ses.ID, accepted.ID)
	if err != nil {
		a.problem(w, r, err)
		return
	}
	c.SessionID = ""
	c.NextTurnNo = 0
	a.accepted(w, r, accepted, map[string]any{"schema_version": 1, "conversation": c}, "/api/v1/conversations/"+c.ID)
}

func (a *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	c, err := a.Store.Conversation(r.Context(), ses.ID, r.PathValue("conversation"))
	if err != nil {
		a.problem(w, r, err)
		return
	}
	c.SessionID = ""
	c.NextTurnNo = 0
	a.json(w, 200, map[string]any{"schema_version": 1, "conversation": c})
}
