package server

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/store"
)

// ---- panel API ----
//
// Every handler below runs behind require(), and every store-scoped one asks
// the principal first: a store the user has no membership for is reported as
// 404 (never 403) so the panel does not confirm that it exists.

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

// visibleStores is the sorted set of stores this session may touch, restricted
// to those that actually have a flow on disk.
func (s *Server) visibleStoreIDs(p *principal) []string {
	ids := make([]string, 0, len(p.Stores))
	for id := range p.Stores {
		if _, ok := s.Eng.Flows[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (p *principal) canStore(id string) bool { return p.Stores[id] }

// apiMe gives the panel the identity and the tenants it is showing.
func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	writeJSON(w, 200, map[string]any{
		"email":   p.User.Email,
		"tenants": p.Tenants,
		"stores":  s.visibleStoreIDs(p),
	})
}

func (s *Server) apiFlows(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out := []flowView{} // an empty JSON array, not null: the panel iterates it
	for _, id := range s.visibleStoreIDs(p) {
		f := s.Eng.Flows[id]
		over, err := s.Eng.Store.StepOverrides(r.Context(), id)
		if err != nil {
			fail(w, 500, err)
			return
		}
		counts, err := s.Eng.Store.StepCounts(r.Context(), id)
		if err != nil {
			fail(w, 500, err)
			return
		}
		fv := flowView{Store: f.Store, Sender: s.SenderName}
		for _, st := range f.Steps {
			eff := st.Status
			if v, ok := over[st.ID]; ok && st.Status != flow.StatusDraft {
				eff = v
			}
			fv.Steps = append(fv.Steps, stepView{Step: st, Effective: eff, Timing: st.Timing(), Counts: counts[st.ID]})
		}
		out = append(out, fv)
	}
	writeJSON(w, 200, out)
}

// flowFor returns the flow, but only when the store belongs to one of the
// session's tenants. Everything else is "unknown store".
func (s *Server) flowFor(w http.ResponseWriter, r *http.Request) (*flow.Flow, bool) {
	id := r.PathValue("store")
	if !principalFrom(r).canStore(id) {
		fail(w, 404, errors.New("unknown store"))
		return nil, false
	}
	f, ok := s.Eng.Flows[id]
	if !ok {
		fail(w, 404, errors.New("unknown store"))
		return nil, false
	}
	return f, true
}

func (s *Server) apiStepStatus(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFor(w, r)
	if !ok {
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
	s.Log.Info("step status changed", "store", f.Store.ID, "step", st.ID, "status", in.Status, "by", principalFrom(r).User.Email)
	writeJSON(w, 200, map[string]string{"status": in.Status})
}

func (s *Server) apiPreview(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFor(w, r)
	if !ok {
		return
	}
	subject, body, err := s.Eng.Preview(f.Store.ID, r.PathValue("step"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("X-Subject", subject)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src * data:; style-src 'unsafe-inline'")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN") // the panel embeds this in an iframe
	io.WriteString(w, body)
}

func (s *Server) apiTest(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFor(w, r)
	if !ok {
		return
	}
	var in struct{ To string }
	if err := decode(r, &in); err != nil || !strings.Contains(in.To, "@") {
		fail(w, 400, errors.New("`to` must be an email address"))
		return
	}
	if err := s.Eng.TestSend(r.Context(), f.Store.ID, r.PathValue("step"), in.To); err != nil {
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

// apiOrders lists orders of the session's stores. A ?store= filter for a store
// the session cannot see is a 404, not an empty list.
func (s *Server) apiOrders(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	filter := r.URL.Query().Get("store")
	if filter != "" && !p.canStore(filter) {
		fail(w, 404, errors.New("unknown store"))
		return
	}
	var orders []store.Order
	if filter != "" {
		list, err := s.Eng.Store.ListOrders(r.Context(), filter, 50)
		if err != nil {
			fail(w, 500, err)
			return
		}
		orders = list
	} else {
		for _, id := range s.visibleStoreIDs(p) {
			list, err := s.Eng.Store.ListOrders(r.Context(), id, 50)
			if err != nil {
				fail(w, 500, err)
				return
			}
			orders = append(orders, list...)
		}
		sort.Slice(orders, func(i, j int) bool { return orders[i].PaidAt.After(orders[j].PaidAt) })
		if len(orders) > 50 {
			orders = orders[:50]
		}
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
	if !principalFrom(r).canStore(in.Store) {
		fail(w, 404, errors.New("unknown store"))
		return
	}
	if _, ok := s.Eng.Flows[in.Store]; !ok {
		fail(w, 404, errors.New("unknown store"))
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

// orderFor loads the order and confirms its store is visible to this session.
func (s *Server) orderFor(w http.ResponseWriter, r *http.Request) (*store.Order, bool) {
	id, err := orderID(r)
	if err != nil {
		fail(w, 404, errors.New("unknown order"))
		return nil, false
	}
	o, err := s.Eng.Store.OrderByID(r.Context(), id)
	if err != nil || !principalFrom(r).canStore(o.StoreID) {
		fail(w, 404, errors.New("unknown order"))
		return nil, false
	}
	return o, true
}

func (s *Server) apiTracking(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orderFor(w, r)
	if !ok {
		return
	}
	var in struct{ Number, Carrier, URL string }
	if err := decode(r, &in); err != nil {
		fail(w, 400, errors.New("bad request"))
		return
	}
	if err := s.Eng.Dispatched(r.Context(), o.ID, strings.TrimSpace(in.Number), in.Carrier, in.URL, time.Now()); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) apiEvent(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orderFor(w, r)
	if !ok {
		return
	}
	var in struct{ Event, Label string }
	if err := decode(r, &in); err != nil {
		fail(w, 400, errors.New("bad request"))
		return
	}
	if !validEvent(in.Event) {
		fail(w, 400, errors.New("unknown event "+in.Event))
		return
	}
	if err := s.Eng.TrackingEvent(r.Context(), o.ID, in.Event, in.Label, "panel", time.Now()); err != nil {
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
	o, ok := s.orderFor(w, r)
	if !ok {
		return
	}
	if err := s.Eng.CancelOrder(r.Context(), o.ID, "canceled from the panel"); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) apiRetry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, 404, errors.New("unknown job"))
		return
	}
	j, err := s.Eng.Store.JobByID(r.Context(), id)
	if err != nil || !principalFrom(r).canStore(j.StoreID) {
		fail(w, 404, errors.New("unknown job"))
		return
	}
	if err := s.Eng.Store.Retry(r.Context(), id, s.Eng.Now()); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
