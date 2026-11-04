// Package server exposes Shopify webhooks, the authenticated panel and its API.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/icl00ud/mailflow/internal/auth"
	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/shopify"
	"github.com/icl00ud/mailflow/internal/store"
	"github.com/icl00ud/mailflow/internal/track17"
)

//go:embed web
var webFS embed.FS

// DefaultOrigin is the public panel origin. It is used for the CSRF check and
// for passkey registration when PUBLIC_ORIGIN is not set, so a misconfigured
// deployment fails closed (cross-origin posts stay rejected) instead of open.
const DefaultOrigin = "https://notify.armancurren.com"

type Server struct {
	Eng        *engine.Engine
	Log        *slog.Logger
	SenderName string
	// ShopifySecret returns the webhook secret of a store ("" when not configured).
	ShopifySecret func(storeID string) string
	// Track17Key authenticates 17TRACK pushes (empty disables /webhooks/17track).
	Track17Key string

	// PublicOrigin is scheme://host of the panel; every state-changing request
	// outside /webhooks must come from it.
	PublicOrigin string
	// CookieSecure sets the Secure flag on the session cookie (default true in main).
	CookieSecure bool
	// WebAuthn is nil when passkeys are not configured; the endpoints then 503.
	WebAuthn *webauthn.WebAuthn
	// Now is swappable in tests.
	Now func() time.Time
	// Limiter throttles password logins.
	Limiter *auth.Limiter
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) origin() string {
	o := strings.TrimSpace(s.PublicOrigin)
	if o == "" {
		o = DefaultOrigin
	}
	return strings.TrimRight(strings.ToLower(o), "/")
}

func (s *Server) limiter() *auth.Limiter {
	if s.Limiter == nil {
		s.Limiter = auth.NewLimiter(10, 15*time.Minute)
	}
	return s.Limiter
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Eng.Store.Ping(r.Context()); err != nil {
			http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /webhooks/shopify", s.shopifyWebhook)
	mux.HandleFunc("POST /webhooks/17track", s.track17Webhook)

	// Public: login and invite acceptance. Both are CSRF-checked in the middleware.
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginPost)
	mux.HandleFunc("GET /invite/{token}", s.invitePage)
	mux.HandleFunc("POST /invite/{token}", s.invitePost)
	mux.HandleFunc("POST /api/passkeys/login/begin", s.passkeyLoginBegin)
	mux.HandleFunc("POST /api/passkeys/login/finish", s.passkeyLoginFinish)
	mux.Handle("GET /static/", s.static())

	// Authenticated panel.
	mux.HandleFunc("GET /{$}", s.require(s.panelPage))
	mux.HandleFunc("GET /account", s.require(s.accountPage))
	mux.HandleFunc("POST /logout", s.require(s.logout))
	mux.HandleFunc("GET /api/me", s.require(s.apiMe))
	mux.HandleFunc("GET /api/flows", s.require(s.apiFlows))
	mux.HandleFunc("POST /api/flows/{store}/steps/{step}/status", s.require(s.apiStepStatus))
	mux.HandleFunc("GET /api/flows/{store}/steps/{step}/preview", s.require(s.apiPreview))
	mux.HandleFunc("POST /api/flows/{store}/steps/{step}/test", s.require(s.apiTest))
	mux.HandleFunc("GET /api/orders", s.require(s.apiOrders))
	mux.HandleFunc("POST /api/orders", s.require(s.apiCreateOrder))
	mux.HandleFunc("POST /api/orders/{id}/tracking", s.require(s.apiTracking))
	mux.HandleFunc("POST /api/orders/{id}/events", s.require(s.apiEvent))
	mux.HandleFunc("POST /api/orders/{id}/cancel", s.require(s.apiCancel))
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.require(s.apiRetry))
	mux.HandleFunc("GET /api/passkeys", s.require(s.apiPasskeys))
	mux.HandleFunc("POST /api/passkeys/register/begin", s.require(s.passkeyRegisterBegin))
	mux.HandleFunc("POST /api/passkeys/register/finish", s.require(s.passkeyRegisterFinish))
	mux.HandleFunc("POST /api/passkeys/{id}/delete", s.require(s.apiPasskeyDelete))

	var h http.Handler = mux
	h = s.securityHeaders(h)
	h = s.csrf(h)
	return s.logRequests(h)
}

// securityHeaders applies the panel's browser hardening. The markup carries no
// inline <script>; everything executable is served from /static. 'unsafe-inline'
// is allowed for styles only, because the email preview is rendered as a
// sandboxed srcdoc frame that inherits this policy.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; "+
			"frame-ancestors 'none'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// csrf rejects every state-changing panel request that did not come from
// PUBLIC_ORIGIN. Webhooks authenticate themselves with HMAC/signature and are
// exempt. Origin wins over Referer when both are present.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !stateChanging(r.Method) || strings.HasPrefix(r.URL.Path, "/webhooks/") {
			next.ServeHTTP(w, r)
			return
		}
		if origin, ok := s.requestOrigin(r); !ok || origin != s.origin() {
			s.Log.Warn("csrf rejected", "path", r.URL.Path, "origin", r.Header.Get("Origin"), "referer", r.Header.Get("Referer"))
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func stateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// requestOrigin returns the normalised origin of the request, preferring Origin.
func (s *Server) requestOrigin(r *http.Request) (string, bool) {
	if o := r.Header.Get("Origin"); o != "" {
		return normaliseOrigin(o)
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return normaliseOrigin(ref)
	}
	return "", false
}

func normaliseOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || raw == "null" {
		return "", false
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" && !strings.HasPrefix(r.URL.Path, "/static/") {
			s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "ms", time.Since(start).Milliseconds())
		}
	})
}

func (s *Server) static() http.Handler {
	sub, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", cacheStatic(http.FileServerFS(sub)))
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

// ---- Shopify ----

func (s *Server) storeByShop(domain string) (*flow.Flow, bool) {
	for _, f := range s.Eng.Flows {
		if f.Store.ShopifyDomain != "" && strings.EqualFold(f.Store.ShopifyDomain, domain) {
			return f, true
		}
	}
	return nil, false
}

func (s *Server) shopifyWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f, ok := s.storeByShop(r.Header.Get("X-Shopify-Shop-Domain"))
	if !ok {
		s.Log.Warn("webhook for unknown shop", "shop", r.Header.Get("X-Shopify-Shop-Domain"))
		http.Error(w, "unknown shop", http.StatusNotFound)
		return
	}
	if !shopify.Verify(body, r.Header.Get("X-Shopify-Hmac-Sha256"), s.ShopifySecret(f.Store.ID)) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	topic := r.Header.Get("X-Shopify-Topic")
	ctx := r.Context()
	st := s.Eng.Store
	var herr error
	switch topic {
	case "orders/paid":
		var o *store.Order
		if o, herr = shopify.Order(f.Store.ID, body); herr == nil {
			_, herr = s.Eng.OrderPaid(ctx, o)
		}
	case "orders/cancelled":
		var ext string
		if ext, herr = shopify.OrderID(body); herr == nil {
			if o, e := st.OrderByExternal(ctx, f.Store.ID, ext); e == nil {
				herr = s.Eng.CancelOrder(ctx, o.ID, "order cancelled in Shopify")
			}
		}
	case "fulfillments/create", "fulfillments/update":
		var fu *shopify.Fulfillment
		if fu, herr = shopify.Parse(body); herr == nil {
			o, e := st.OrderByExternal(ctx, f.Store.ID, fu.OrderExternalID)
			if e != nil {
				s.Log.Warn("fulfillment for unknown order", "order", fu.OrderExternalID)
				break
			}
			if fu.Number != "" {
				herr = s.Eng.Dispatched(ctx, o.ID, fu.Number, fu.Carrier, fu.URL, time.Now())
			}
			if herr == nil && fu.Event != "" {
				herr = s.Eng.TrackingEvent(ctx, o.ID, fu.Event, fu.ShipmentStatus, "shopify", time.Now())
			}
		}
	default:
		s.Log.Info("ignored webhook topic", "topic", topic)
	}
	if herr != nil {
		s.Log.Error("webhook", "topic", topic, "err", herr)
		http.Error(w, "processing error", http.StatusInternalServerError) // Shopify retries
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) track17Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !track17.Verify(body, r.Header.Get("sign"), s.Track17Key) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	p, err := track17.Parse(body)
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	orderID, ok := track17.OrderFromTag(p.Tag)
	if !ok { // not ours (or tag lost): acknowledge so 17TRACK stops retrying
		s.Log.Warn("17track push without a mailflow tag", "number", p.Number)
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := s.Eng.Store.OrderByID(ctx, orderID); err != nil {
		s.Log.Warn("17track push for unknown order", "order", orderID)
		w.WriteHeader(http.StatusOK)
		return
	}
	for _, ev := range p.Events {
		if err := s.Eng.TrackingEvent(ctx, orderID, ev.Name, ev.Label, "17track", ev.At); err != nil {
			s.Log.Error("17track event", "order", orderID, "event", ev.Name, "err", err)
			http.Error(w, "processing error", http.StatusInternalServerError) // 17TRACK retries
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// SecretFromEnv resolves SHOPIFY_WEBHOOK_SECRET_<STORE_ID>, falling back to SHOPIFY_WEBHOOK_SECRET.
func SecretFromEnv(storeID string) string {
	key := "SHOPIFY_WEBHOOK_SECRET_" + strings.ToUpper(strings.ReplaceAll(storeID, "-", "_"))
	if v := os.Getenv(key); v != "" {
		return v
	}
	return os.Getenv("SHOPIFY_WEBHOOK_SECRET")
}

// errNoSession means "not logged in" (no cookie, unknown or expired row).
var errNoSession = errors.New("no session")
