package server

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"time"

	"github.com/icl00ud/mailflow/internal/store"
)

var pages = template.Must(template.ParseFS(webFS, "web/*.html"))

type pageData struct {
	Title           string
	Email           string
	Error           string
	Next            string
	Token           string
	PasswordMin     int
	Welcome         bool
	Tenants         []tenantView
	PasskeysEnabled bool
}

type passkeyView struct {
	ID         string     `json:"id"` // base64url, as the browser and the API use it
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func newPasskeyView(k store.Passkey) passkeyView {
	name := k.Name
	if name == "" {
		name = "passkey"
	}
	return passkeyView{ID: base64.RawURLEncoding.EncodeToString(k.ID), Name: name, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
}

func decodeCredentialID(s string) ([]byte, bool) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, true
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, true
	}
	return nil, false
}

func (s *Server) render(w http.ResponseWriter, name string, data pageData) {
	s.renderStatus(w, name, data, http.StatusOK)
}

func (s *Server) renderStatus(w http.ResponseWriter, name string, data pageData, code int) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		s.Log.Error("render", "template", name, "err", err)
	}
}
