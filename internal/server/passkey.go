package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/icl00ud/mailflow/internal/store"
)

const challengeTTL = 5 * time.Minute

// waUser adapts a stored user + its credentials to webauthn.User.
type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w waUser) WebAuthnName() string                       { return w.u.Email }
func (w waUser) WebAuthnDisplayName() string                { return w.u.Email }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func credsOf(keys []store.Passkey) []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(keys))
	for _, k := range keys {
		var tr []protocol.AuthenticatorTransport
		if k.Transports != "" {
			_ = json.Unmarshal([]byte(k.Transports), &tr)
		}
		out = append(out, webauthn.Credential{ID: k.ID, PublicKey: k.PublicKey, Transport: tr,
			Authenticator: webauthn.Authenticator{SignCount: uint32(k.SignCount)}})
	}
	return out
}

func (s *Server) waUserFor(ctx context.Context, u *store.User) (waUser, error) {
	keys, err := s.Eng.Store.PasskeysForUser(ctx, u.ID)
	if err != nil {
		return waUser{}, err
	}
	return waUser{u: u, creds: credsOf(keys)}, nil
}

// challengePayload wraps the library's SessionData with the ceremony kind, so
// a registration challenge cannot be replayed at the login endpoint.
type challengePayload struct {
	Kind    string               `json:"kind"`
	Session webauthn.SessionData `json:"session"`
}

func (s *Server) putChallenge(r *http.Request, userID, kind string, session *webauthn.SessionData) (string, error) {
	data, err := json.Marshal(challengePayload{Kind: kind, Session: *session})
	if err != nil {
		return "", err
	}
	id := store.NewID()
	err = s.Eng.Store.PutChallenge(r.Context(), &store.Challenge{ID: id, UserID: userID, Kind: kind,
		Data: data, ExpiresAt: s.now().Add(challengeTTL)})
	return id, err
}

// takeChallenge consumes the ceremony state exactly once. A missing, expired,
// replayed or wrong-kind challenge is a 400 and never a retry.
func (s *Server) takeChallenge(w http.ResponseWriter, r *http.Request, kind string) (*webauthn.SessionData, bool) {
	cid := r.URL.Query().Get("cid")
	if cid == "" {
		fail(w, 400, errors.New("missing challenge"))
		return nil, false
	}
	data, err := s.Eng.Store.TakeChallenge(r.Context(), cid, s.now())
	if err != nil {
		fail(w, 400, errors.New("challenge expired or already used; try again"))
		return nil, false
	}
	var p challengePayload
	if err := json.Unmarshal(data, &p); err != nil || p.Kind != kind {
		fail(w, 400, errors.New("challenge expired or already used; try again"))
		return nil, false
	}
	return &p.Session, true
}

func (s *Server) passkeyUnavailable(w http.ResponseWriter) bool {
	if s.WebAuthn == nil {
		fail(w, http.StatusServiceUnavailable, errors.New("passkeys are not configured"))
		return true
	}
	return false
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if s.passkeyUnavailable(w) {
		return
	}
	p := principalFrom(r)
	wa, err := s.waUserFor(r.Context(), p.User)
	if err != nil {
		fail(w, 500, err)
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, k := range wa.creds {
		exclude = append(exclude, protocol.CredentialDescriptor{Type: protocol.PublicKeyCredentialType, CredentialID: k.ID})
	}
	opts, session, err := s.WebAuthn.BeginRegistration(wa,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		fail(w, 500, err)
		return
	}
	cid, err := s.putChallenge(r, p.User.ID, "register", session)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"challenge_id": cid, "options": opts})
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if s.passkeyUnavailable(w) {
		return
	}
	p := principalFrom(r)
	sd, ok := s.takeChallenge(w, r, "register")
	if !ok {
		return
	}
	if string(sd.UserID) != p.User.ID {
		fail(w, 400, errors.New("challenge belongs to another session"))
		return
	}
	wa, err := s.waUserFor(r.Context(), p.User)
	if err != nil {
		fail(w, 500, err)
		return
	}
	cred, err := s.WebAuthn.FinishRegistration(wa, *sd, r)
	if err != nil {
		s.Log.Warn("passkey registration failed", "email", p.User.Email, "err", err)
		fail(w, 400, errors.New("could not register this passkey"))
		return
	}
	transports, _ := json.Marshal(cred.Transport)
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "passkey"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	k := &store.Passkey{ID: cred.ID, UserID: p.User.ID, PublicKey: cred.PublicKey,
		SignCount: int64(cred.Authenticator.SignCount), Transports: string(transports), Name: name, CreatedAt: s.now()}
	if err := s.Eng.Store.CreatePasskey(r.Context(), k); err != nil {
		fail(w, 500, err)
		return
	}
	s.Log.Info("passkey registered", "email", p.User.Email, "name", name)
	writeJSON(w, 200, newPasskeyView(*k))
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if s.passkeyUnavailable(w) {
		return
	}
	opts, session, err := s.WebAuthn.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		fail(w, 500, err)
		return
	}
	cid, err := s.putChallenge(r, "", "login", session)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"challenge_id": cid, "options": opts})
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if s.passkeyUnavailable(w) {
		return
	}
	sd, ok := s.takeChallenge(w, r, "login")
	if !ok {
		return
	}
	var found *store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.Eng.Store.UserByID(r.Context(), string(userHandle))
		if err != nil {
			return nil, errors.New("unknown user")
		}
		wa, err := s.waUserFor(r.Context(), u)
		if err != nil {
			return nil, err
		}
		found = u
		return wa, nil
	}
	cred, err := s.WebAuthn.FinishDiscoverableLogin(handler, *sd, r)
	if err != nil || found == nil {
		s.Log.Warn("passkey login failed", "err", err)
		fail(w, 401, errors.New("passkey not recognised"))
		return
	}
	stored, err := s.Eng.Store.PasskeyByID(r.Context(), cred.ID)
	if err != nil || stored.UserID != found.ID {
		fail(w, 401, errors.New("passkey not recognised"))
		return
	}
	// Authenticator counter must move forward when it is used at all; a
	// regression means a cloned authenticator.
	newCount := int64(cred.Authenticator.SignCount)
	if newCount > 0 && stored.SignCount > 0 && newCount <= stored.SignCount {
		s.Log.Error("passkey counter regressed; refusing", "email", found.Email, "stored", stored.SignCount, "got", newCount)
		fail(w, 401, errors.New("passkey not recognised"))
		return
	}
	if err := s.Eng.Store.TouchPasskey(r.Context(), cred.ID, newCount, s.now()); err != nil {
		s.Log.Error("touch passkey", "err", err)
	}
	if err := s.startSession(w, r, found.ID); err != nil {
		fail(w, 500, err)
		return
	}
	s.Log.Info("login with passkey", "email", found.Email)
	writeJSON(w, 200, map[string]string{"redirect": "/"})
}
