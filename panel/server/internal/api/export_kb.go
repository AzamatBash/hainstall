package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/azabash/hapanel/panel/internal/buildinfo"
	"github.com/azabash/hapanel/panel/internal/kb"
)

// kbManifest is the GET /api/v1/kb response (schema hapanel.kb.v1).
type kbManifest struct {
	Schema      string              `json:"schema"`
	Version     string              `json:"version"`
	GeneratedAt string              `json:"generated_at"`
	Revision    string              `json:"revision"`
	Title       string              `json:"title"`
	Articles    []kbManifestArticle `json:"articles"`
	Warnings    []kbWarning         `json:"warnings"`
}

type kbManifestArticle struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Order   int    `json:"order"`
	Size    int    `json:"size"`
	SHA256  string `json:"sha256"`
}

type kbWarning struct {
	Code string `json:"code"`
	Key  string `json:"key"`
}

// kbArticle is the GET /api/v1/kb/article response.
type kbArticle struct {
	Schema   string `json:"schema"`
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	SHA256   string `json:"sha256"`
	Size     int    `json:"size"`
	Text     string `json:"text"`
}

// noStore marks every response of a route, errors included, as not to be stored.
func noStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

// handleKB returns the table of contents of the knowledge base in
// PANEL_KB_DIR, read once on the first request. ETag is the revision; there is
// no 304 (the panel client takes neither redirects nor 304).
//
// GET /api/v1/kb (API key with scope "config" or admin JWT)
func (s *Server) handleKB(w http.ResponseWriter, r *http.Request) {
	base, ok := s.kbBase(w)
	if !ok {
		return
	}
	articles := make([]kbManifestArticle, 0, len(base.Articles))
	for _, a := range base.Articles {
		articles = append(articles, kbManifestArticle{Key: a.Key, Title: a.Title, Section: a.Section, Order: a.Order, Size: a.Size, SHA256: a.SHA256})
	}
	warnings := make([]kbWarning, 0, len(base.Warnings))
	for _, wn := range base.Warnings {
		warnings = append(warnings, kbWarning{Code: wn.Code, Key: wn.Key})
	}
	w.Header().Set("ETag", `"`+base.Revision+`"`)
	writeJSON(w, http.StatusOK, kbManifest{
		Schema:      kb.Schema,
		Version:     buildinfo.Version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    base.Revision,
		Title:       base.Title,
		Articles:    articles,
		Warnings:    warnings,
	})
}

// handleKBArticle returns an article of the table of contents: the file bytes
// as is, with the revision of the table of contents it belongs to.
//
// GET /api/v1/kb/article?key=a/<name>.txt (API key with scope "config" or admin JWT)
func (s *Server) handleKBArticle(w http.ResponseWriter, r *http.Request) {
	keys := r.URL.Query()["key"]
	if len(keys) == 0 || (len(keys) == 1 && keys[0] == "") {
		writeErr(w, http.StatusBadRequest, "не указан ключ статьи")
		return
	}
	if len(keys) != 1 || !kb.ValidKey(keys[0]) {
		writeErr(w, http.StatusBadRequest, "неверный ключ статьи")
		return
	}
	base, ok := s.kbBase(w)
	if !ok {
		return
	}
	a, found := base.Article(keys[0])
	if !found {
		writeErr(w, http.StatusNotFound, "статья не найдена")
		return
	}
	writeJSON(w, http.StatusOK, kbArticle{
		Schema:   kb.Schema,
		Version:  buildinfo.Version,
		Revision: base.Revision,
		Key:      a.Key,
		Title:    a.Title,
		SHA256:   a.SHA256,
		Size:     a.Size,
		Text:     a.Text,
	})
}

// kbBase returns the loaded base, or writes 404 when there is none and 500
// when it could not be read (logged; the next request reads it again).
func (s *Server) kbBase(w http.ResponseWriter) (*kb.Base, bool) {
	base, err := s.kb.Base()
	switch {
	case err == nil:
		return base, true
	case errors.Is(err, kb.ErrNotFound):
		writeErr(w, http.StatusNotFound, "база знаний недоступна")
	default:
		s.logger.Error("kb load", "dir", s.kb.Dir(), "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка чтения базы знаний")
	}
	return nil, false
}
