package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/auth"
	"github.com/icl00ud/mailflow/internal/store"
)

const (
	sessionCookie = "mf_session"
	sessionTTL    = 30 * 24 * time.Hour
	minPassword   = 12
	inviteTTL     = 48 * time.Hour
)

// principal is the logged-in identity plus what it is allowed to see, loaded
// once per request.
type principal struct {
	User        *store.User
	Stores      map[string]bool
	Memberships []store.Membership
	Tenants     []tenantView
}

type tenantView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type principalKey struct{}

func principalFrom(r *http.Request) *principal {
	p, _ := r.Context().Value(principalKey{}).(*principal)
	return p
}

func (s *Server) loadPrincipal(r *http.Request) (*principal, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, errNoSession
	}
	st := s.Eng.Store
	sess, err := st.Session(r.Context(), auth.HashToken(c.Value), s.now())
	if err != nil {
		return nil, errNoSession
	}
	u, err := st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		return nil, errNoSession
	}
	ids, err := st.StoreIDsForUser(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	mems, err := st.Memberships(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	p := &principal{User: u, Stores: make(map[string]bool, len(ids)), Memberships: mems}
	for _, id := range ids {
		p.Stores[id] = true
	}
	for _, m := range mems {
		t, err := st.TenantByID(r.Context(), m.TenantID)
		if err != nil {
			p.Tenants = append(p.Tenants, tenantView{ID: m.TenantID, Role: m.Role})
			continue
		}
		p.Tenants = append(p.Tenants, tenantView{ID: t.ID, Name: t.Name, Role: m.Role})
	}
	return p, nil
}

// require gates a handler on a valid session. API calls get 401 JSON; pages are
// redirected to /login with a same-origin `next`.
func (s *Server) require(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.loadPrincipal(r)
		if err != nil {
			if err != errNoSession {
				s.Log.Error("load session", "err", err)
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				fail(w, http.StatusUnauthorized, errors.New("unauthorized"))
				return
			}
			target := "/login"
			if r.Method == http.MethodGet && r.URL.Path != "/" {
				target += "?next=" + url.QueryEscape(r.URL.Path)
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}

// ---- login / logout ----

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, err := s.loadPrincipal(r); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", pageData{
		Title:           "Entrar — mailflow",
		Next:            safeNext(r.URL.Query().Get("next")),
		PasskeysEnabled: s.WebAuthn != nil,
	})
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "login.html", pageData{Title: "Entrar — mailflow", Error: msgBadBody, PasskeysEnabled: s.WebAuthn != nil})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))
	lim := s.limiter()
	key := s.clientIP(r) + "|" + email
	now := s.now()

	if !lim.Allow(key, now) {
		s.renderStatus(w, "login.html", pageData{Title: "Entrar — mailflow", Next: next, Email: email,
			Error: msgTooMany, PasskeysEnabled: s.WebAuthn != nil}, http.StatusTooManyRequests)
		return
	}
	u, err := s.Eng.Store.UserByEmail(r.Context(), email)
	hash := auth.DummyHash
	if err == nil && u.PasswordHash != "" {
		hash = u.PasswordHash
	}
	// Verify even for unknown users / users without a password: same cost, same answer.
	ok := auth.VerifyPassword(hash, password)
	if err != nil || u == nil || u.PasswordHash == "" || !ok {
		lim.Fail(key, now)
		s.Log.Warn("login failed", "email", email)
		s.renderStatus(w, "login.html", pageData{Title: "Entrar — mailflow", Next: next, Email: email,
			Error: msgBadLogin, PasskeysEnabled: s.WebAuthn != nil}, http.StatusUnauthorized)
		return
	}
	lim.Reset(key)
	if err := s.startSession(w, r, u.ID); err != nil {
		s.Log.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.Log.Info("login", "email", u.Email)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

const (
	msgBadLogin = "Email ou senha incorretos."
	msgBadBody  = "Requisição inválida."
	msgTooMany  = "Muitas tentativas. Tente novamente em alguns minutos."
)

// startSession mints a token, stores its hash and sets the cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	now := s.now()
	sess := &store.Session{TokenHash: auth.HashToken(token), UserID: userID, ExpiresAt: now.Add(sessionTTL), CreatedAt: now}
	if err := s.Eng.Store.CreateSession(r.Context(), sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
		MaxAge:   int(sessionTTL / time.Second),
	})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.Eng.Store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.Log.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.CookieSecure,
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0), MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// safeNext only accepts an absolute path on this host: no scheme, no
// protocol-relative "//host", and no backslash that a browser would fold into
// a second slash.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") ||
		strings.ContainsAny(next, "\\\r\n	") {
		return "/"
	}
	return next
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- invite ----

func (s *Server) invitePage(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.inviteFromPath(w, r)
	if !ok {
		return
	}
	s.render(w, "invite.html", pageData{Title: "Definir senha — mailflow", Email: inv.Email, Token: r.PathValue("token"),
		PasswordMin: minPassword, PasskeysEnabled: s.WebAuthn != nil})
}

func (s *Server) invitePost(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.inviteFromPath(w, r)
	if !ok {
		return
	}
	data := pageData{Title: "Definir senha — mailflow", Email: inv.Email, Token: r.PathValue("token"),
		PasswordMin: minPassword, PasskeysEnabled: s.WebAuthn != nil}
	if err := r.ParseForm(); err != nil {
		data.Error = msgBadBody
		s.render(w, "invite.html", data)
		return
	}
	pw, confirm := r.PostFormValue("password"), r.PostFormValue("password_confirm")
	switch {
	case len(pw) < minPassword:
		data.Error = "A senha precisa ter pelo menos 12 caracteres."
	case pw != confirm:
		data.Error = "As senhas não coincidem."
	}
	if data.Error != "" {
		s.render(w, "invite.html", data)
		return
	}
	// Claim the invite first: ConsumeInvite is atomic, so two tabs (or a leaked
	// link) cannot both set a password.
	consumed, err := s.Eng.Store.ConsumeInvite(r.Context(), inv.ID, s.now())
	if err != nil {
		s.Log.Error("consume invite", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !consumed {
		s.renderExpiredInvite(w)
		return
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	u, err := s.Eng.Store.UserByEmail(r.Context(), inv.Email)
	if errors.Is(err, store.ErrNotFound) {
		u, err = s.Eng.Store.CreateUser(r.Context(), inv.Email)
	}
	if errors.Is(err, store.ErrExists) { // created concurrently
		u, err = s.Eng.Store.UserByEmail(r.Context(), inv.Email)
	}
	if err != nil {
		s.Log.Error("invite user", "email", inv.Email, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.Eng.Store.SetPasswordHash(r.Context(), u.ID, hash); err != nil {
		s.Log.Error("set password", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.Eng.Store.AddMembership(r.Context(), u.ID, inv.TenantID, inv.Role); err != nil {
		s.Log.Error("add membership", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		s.Log.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.Log.Info("invite accepted", "email", u.Email, "tenant", inv.TenantID)
	http.Redirect(w, r, "/account?welcome=1", http.StatusSeeOther)
}

func (s *Server) inviteFromPath(w http.ResponseWriter, r *http.Request) (*store.Invite, bool) {
	token := r.PathValue("token")
	if token == "" {
		s.renderExpiredInvite(w)
		return nil, false
	}
	inv, err := s.Eng.Store.InviteByTokenHash(r.Context(), auth.HashToken(token), s.now())
	if err != nil {
		s.renderExpiredInvite(w)
		return nil, false
	}
	return inv, true
}

func (s *Server) renderExpiredInvite(w http.ResponseWriter) {
	s.renderStatus(w, "invite_gone.html", pageData{Title: "Convite inválido — mailflow"}, http.StatusGone)
}

// ---- account ----

func (s *Server) apiPasskeys(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	list, err := s.Eng.Store.PasskeysForUser(r.Context(), p.User.ID)
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := make([]passkeyView, 0, len(list))
	for _, k := range list {
		out = append(out, newPasskeyView(k))
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiPasskeyDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := decodeCredentialID(r.PathValue("id"))
	if !ok {
		fail(w, 400, errors.New("bad credential id"))
		return
	}
	deleted, err := s.Eng.Store.DeletePasskey(r.Context(), id, principalFrom(r).User.ID)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if !deleted {
		fail(w, 404, errors.New("unknown passkey"))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
