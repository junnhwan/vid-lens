package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (a *Server) publicExamples(w http.ResponseWriter, r *http.Request) {
	items, e := a.Store.PublicExamples(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.json(w, http.StatusOK, map[string]any{"schema_version": 1, "items": items})
}

func (a *Server) publicExample(w http.ResponseWriter, r *http.Request) {
	if e := a.Store.FreezeExampleProcess(r.Context(), r.PathValue("article")); e != nil {
		a.problem(w, r, e)
		return
	}
	item, e := a.Store.PublicExample(r.Context(), r.PathValue("article"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a.json(w, http.StatusOK, item)
}

func (a *Server) exampleConversation(w http.ResponseWriter, r *http.Request) {
	ses, ok := a.auth(w, r)
	if !ok {
		return
	}
	if !a.body(w, r, &struct{}{}) {
		return
	}
	conversation, created, e := a.Store.OpenExampleConversation(r.Context(), ses.ID, r.PathValue("article"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	conversation.SessionID = ""
	conversation.NextTurnNo = 0
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	a.json(w, status, map[string]any{"schema_version": 1, "conversation": conversation, "question_limit": store.ExampleQuestionLimit})
}

func (a *Server) publicExampleFrame(w http.ResponseWriter, r *http.Request) {
	variant := r.URL.Query().Get("variant")
	if variant != "" && variant != "full" && variant != "thumbnail" {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "图片 variant 无效。"))
		return
	}
	if e := a.Store.FreezeExampleProcess(r.Context(), r.PathValue("article")); e != nil {
		a.problem(w, r, e)
		return
	}
	asset, e := a.Store.PublicExampleFrame(r.Context(), r.PathValue("article"), r.PathValue("frame"))
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	a.serveStoredAsset(w, r, asset)
}

func (a *Server) adminExamples(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	items, e := a.Store.AdminExamples(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, http.StatusOK, map[string]any{"schema_version": 1, "items": items})
}

func (a *Server) saveExamples(w http.ResponseWriter, r *http.Request) {
	if !a.admin(w, r) {
		return
	}
	var body struct {
		ArticleIDs *[]string `json:"article_ids"`
	}
	if !a.body(w, r, &body) {
		return
	}
	if body.ArticleIDs == nil {
		a.problem(w, r, domain.Err(400, "INVALID_ARGUMENT", "请提供要展示的图文。"))
		return
	}
	if e := a.Store.ReplaceExampleShowcases(r.Context(), *body.ArticleIDs); e != nil {
		a.problem(w, r, e)
		return
	}
	items, e := a.Store.AdminExamples(r.Context())
	if e != nil {
		a.problem(w, r, e)
		return
	}
	a.json(w, http.StatusOK, map[string]any{"schema_version": 1, "items": items})
}

func (a *Server) serveStoredAsset(w http.ResponseWriter, r *http.Request, asset domain.Asset) {
	path := filepath.Join(a.Config.DataDir, filepath.Clean(asset.StorageKey))
	if !strings.HasPrefix(path, a.Config.DataDir+string(os.PathSeparator)) {
		a.problem(w, r, errors.New("invalid storage key"))
		return
	}
	file, e := os.Open(path)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) {
			_ = a.Store.MarkMissingFrame(r.Context(), asset.ID)
		}
		a.problem(w, r, domain.Err(503, "SERVICE_UNAVAILABLE", "图片暂不可读。"))
		return
	}
	defer file.Close()
	stat, e := file.Stat()
	if e != nil {
		a.problem(w, r, e)
		return
	}
	w.Header().Set("Content-Type", asset.MIME)
	http.ServeContent(w, r, asset.ID, stat.ModTime(), file)
}
