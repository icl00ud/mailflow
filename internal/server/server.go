// Package server exposes Shopify webhooks, the admin API and the flow panel.
package server

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/shopify"
	"github.com/icl00ud/mailflow/internal/store"
	"github.com/icl00ud/mailflow/internal/track17"
)

//go:embed web/index.html
var web embed.FS

type Server struct {
	Eng           *engine.Engine
	Log           *slog.Logger
	AdminPassword string
	// ShopifySecret returns the webhook secret of a store ("" when not configured).
	ShopifySecret func(storeID string) string
	SenderName    string
	// Track17Key authenticates 17TRACK pushes (empty disables /webhooks/17track).
	Track17Key string
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

	admin := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }
	admin("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := web.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	admin("GET /api/flows", s.apiFlows)
	admin("POST /api/flows/{store}/steps/{step}/status", s.apiStepStatus)
	admin("GET /api/flows/{store}/steps/{step}/preview", s.apiPreview)
	admin("POST /api/flows/{store}/steps/{step}/test", s.apiTest)
	admin("GET /api/orders", s.apiOrders)
	admin("POST /api/orders", s.apiCreateOrder)
	admin("POST /api/orders/{id}/tracking", s.apiTracking)
	admin("POST /api/orders/{id}/events", s.apiEvent)
	admin("POST /api/orders/{id}/cancel", s.apiCancel)
	admin("POST /api/jobs/{id}/retry", s.apiRetry)
	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" {
			s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "ms", time.Since(start).Milliseconds())
		}
	})
}

// auth is HTTP Basic (user "admin"). State-changing calls must be JSON so a
// cross-site form post cannot ride the browser's cached credentials.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(s.AdminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="mailflow"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		next(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
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

// ---- panel API ----

type stepView struct {
	flow.Step
	Effective string         `json:"effective"` // on | off | draft
	Timing    string         `json:"timing"`
	Counts    map[string]int `json:"counts"`
}

type flowView struct {
	Store  flow.StoreCfg `json:"store"`
	Steps  []stepView    `json:"steps"`
	Sender string        `json:"sender"`
}

func (s *Server) apiFlows(w http.ResponseWriter, r *http.Request) {
	var out []flowView
	ids := make([]string, 0, len(s.Eng.Flows))
	for id := range s.Eng.Flows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := s.Eng.Flows[id]
		over, err := s.Eng.Store.StepOverrides(r.Context(), id)
		if err == nil {
			var counts store.StepCounts
			if counts, err = s.Eng.Store.StepCounts(r.Context(), id); err == nil {
				fv := flowView{Store: f.Store, Sender: s.SenderName}
				for _, st := range f.Steps {
					eff := st.Status
					if v, ok := over[st.ID]; ok && st.Status != flow.StatusDraft {
						eff = v
					}
					fv.Steps = append(fv.Steps, stepView{Step: st, Effective: eff, Timing: st.Timing(), Counts: counts[st.ID]})
				}
				out = append(out, fv)
				continue
			}
		}
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiStepStatus(w http.ResponseWriter, r *http.Request) {
	f, ok := s.Eng.Flows[r.PathValue("store")]
	if !ok {
		fail(w, 404, errors.New("unknown store"))
		return
	}
	st, ok := f.Step(r.PathValue("step"))
	if !ok || st.Status == flow.StatusDraft {
		fail(w, 400, errors.New("unknown step, or a draft (write the copy first)"))
		return
	}
	var in struct{ Status string }
	if err := decode(r, &in); err != nil || (in.Status != flow.StatusOn && in.Status != flow.StatusOff) {
		fail(w, 400, errors.New(`status must be "on" or "off"`))
		return
	}
	if err := s.Eng.Store.SetStepOverride(r.Context(), f.Store.ID, st.ID, in.Status); err != nil {
		fail(w, 500, err)
		return
	}
	s.Log.Info("step status changed", "store", f.Store.ID, "step", st.ID, "status", in.Status)
	writeJSON(w, 200, map[string]string{"status": in.Status})
}

func (s *Server) apiPreview(w http.ResponseWriter, r *http.Request) {
	subject, body, err := s.Eng.Preview(r.PathValue("store"), r.PathValue("step"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("X-Subject", subject)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src * data:; style-src 'unsafe-inline'")
	io.WriteString(w, body)
}

func (s *Server) apiTest(w http.ResponseWriter, r *http.Request) {
	var in struct{ To string }
	if err := decode(r, &in); err != nil || !strings.Contains(in.To, "@") {
		fail(w, 400, errors.New("`to` must be an email address"))
		return
	}
	if err := s.Eng.TestSend(r.Context(), r.PathValue("store"), r.PathValue("step"), in.To); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]string{"sent_to": in.To, "via": s.SenderName})
}

type orderView struct {
	store.Order
	Jobs   []store.Job   `json:"jobs"`
	Events []store.Event `json:"events"`
}

func (s *Server) apiOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := s.Eng.Store.ListOrders(r.Context(), r.URL.Query().Get("store"), 50)
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := make([]orderView, 0, len(orders))
	for _, o := range orders {
		jobs, _ := s.Eng.Store.OrderJobs(r.Context(), o.ID)
		evs, _ := s.Eng.Store.Events(r.Context(), o.ID)
		out = append(out, orderView{Order: o, Jobs: jobs, Events: evs})
	}
	writeJSON(w, 200, out)
}

// apiCreateOrder registers a manual order — for testing the journey without Shopify.
func (s *Server) apiCreateOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Store, Email, Name, Customer, Item, Total string
	}
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if _, ok := s.Eng.Flows[in.Store]; !ok {
		fail(w, 400, errors.New("unknown store"))
		return
	}
	now := s.Eng.Now()
	if in.Item == "" {
		in.Item = "Azure Chronograph Leather"
	}
	if in.Total == "" {
		in.Total = "$104.99"
	}
	o := &store.Order{StoreID: in.Store, ExternalID: "manual-" + strconv.FormatInt(now.UnixNano(), 36),
		Name: in.Name, Email: in.Email, CustomerName: in.Customer, Currency: "USD", Total: in.Total, Country: "US",
		PaidAt: now, Items: []store.Item{{Title: in.Item, Qty: 1}}}
	if o.Name == "" {
		o.Name = "#T" + strconv.FormatInt(now.Unix()%100000, 10)
	}
	if _, err := s.Eng.OrderPaid(r.Context(), o); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 201, o)
}

func orderID(r *http.Request) (int64, error) { return strconv.ParseInt(r.PathValue("id"), 10, 64) }

func (s *Server) apiTracking(w http.ResponseWriter, r *http.Request) {
	id, err := orderID(r)
	var in struct{ Number, Carrier, URL string }
	if err != nil || decode(r, &in) != nil {
		fail(w, 400, errors.New("bad request"))
		return
	}
	if err := s.Eng.Dispatched(r.Context(), id, strings.TrimSpace(in.Number), in.Carrier, in.URL, time.Now()); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) apiEvent(w http.ResponseWriter, r *http.Request) {
	id, err := orderID(r)
	var in struct{ Event, Label string }
	if err != nil || decode(r, &in) != nil {
		fail(w, 400, errors.New("bad request"))
		return
	}
	if !validEvent(in.Event) {
		fail(w, 400, fmt.Errorf("unknown event %q", in.Event))
		return
	}
	if err := s.Eng.TrackingEvent(r.Context(), id, in.Event, in.Label, "panel", time.Now()); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func validEvent(e string) bool {
	switch e {
	case flow.EvInTransit, flow.EvArrivedCountry, flow.EvOutForDelivery, flow.EvReadyForPickup, flow.EvDelivered, flow.EvException:
		return true
	}
	return false
}

func (s *Server) apiCancel(w http.ResponseWriter, r *http.Request) {
	id, err := orderID(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Eng.CancelOrder(r.Context(), id, "canceled from the panel"); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) apiRetry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Eng.Store.Retry(r.Context(), id, s.Eng.Now()); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// SecretFromEnv resolves SHOPIFY_WEBHOOK_SECRET_<STORE_ID>, falling back to SHOPIFY_WEBHOOK_SECRET.
func SecretFromEnv(storeID string) string {
	key := "SHOPIFY_WEBHOOK_SECRET_" + strings.ToUpper(strings.ReplaceAll(storeID, "-", "_"))
	if v := os.Getenv(key); v != "" {
		return v
	}
	return os.Getenv("SHOPIFY_WEBHOOK_SECRET")
}
