package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/store"
)

type capture struct {
	mu   sync.Mutex
	msgs []mail.Message
	fail error
}

func (c *capture) Name() string { return "capture" }
func (c *capture) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return c.fail
	}
	c.msgs = append(c.msgs, m)
	return nil
}
func (c *capture) subjects() []string {
	var s []string
	for _, m := range c.msgs {
		s = append(s, m.Subject)
	}
	return s
}

type env struct {
	e   *Engine
	s   *store.Memory
	c   *capture
	f   *flow.Flow
	now time.Time
	t   *testing.T
}

func setup(t *testing.T) *env {
	f, err := flow.Load("../../stores/arman-curren")
	if err != nil {
		t.Fatal(err)
	}
	s := store.NewMemory()
	c := &capture{}
	e := New(s, map[string]*flow.Flow{f.Store.ID: f}, c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	v := &env{e: e, s: s, c: c, f: f, t: t}
	v.now = time.Date(2026, 10, 21, 11, 0, 0, 0, f.Loc()) // Wednesday
	e.Now = func() time.Time { return v.now }
	if err := e.Render.Preload(f); err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *env) order() *store.Order {
	o := &store.Order{StoreID: "arman-curren", ExternalID: "9001", Name: "#1001", Email: "buyer@example.com",
		CustomerName: "Alex Morgan", Total: "$89.99", PaidAt: v.now, Items: []store.Item{{Title: "Azure Chronograph", Qty: 1}}}
	if _, err := v.e.OrderPaid(context.Background(), o); err != nil {
		v.t.Fatal(err)
	}
	return o
}

func (v *env) at(s string) { // advance clock to "2006-01-02 15:04" ET and tick
	tm, err := time.ParseInLocation("2006-01-02 15:04", s, v.f.Loc())
	if err != nil {
		v.t.Fatal(err)
	}
	v.now = tm
	if err := v.e.Tick(context.Background()); err != nil {
		v.t.Fatal(err)
	}
}

func (v *env) job(orderID int64, step string) store.Job {
	jobs, _ := v.s.OrderJobs(context.Background(), orderID)
	for _, j := range jobs {
		if j.StepID == step {
			return j
		}
	}
	return store.Job{}
}

func TestFullJourney(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	o := v.order()

	v.at("2026-10-21 11:00")
	if got := v.c.subjects(); len(got) != 1 || !strings.Contains(got[0], "#1001 is confirmed") {
		t.Fatalf("after payment: %v", got)
	}
	if j := v.job(o.ID, "2"); !j.DueAt.Equal(time.Date(2026, 10, 22, 11, 0, 0, 0, v.f.Loc())) {
		t.Fatalf("step 2 due %v, want Thu 11:00 (24 exact hours)", j.DueAt)
	}

	v.at("2026-10-22 11:00") // step 2
	if n := len(v.c.msgs); n != 2 {
		t.Fatalf("msgs=%d want 2", n)
	}
	// 2.5 = Thu 11:00 + 24 business h = Fri 11:00
	if j := v.job(o.ID, "2.5"); !j.DueAt.Equal(time.Date(2026, 10, 23, 11, 0, 0, 0, v.f.Loc())) {
		t.Fatalf("step 2.5 due %v, want Fri 11:00", j.DueAt)
	}

	v.at("2026-10-23 11:00") // 2.5 sent; step 3 = Fri 11:00 + 24 business h = Mon 11:00 (weekend skipped)
	if j := v.job(o.ID, "3"); !j.DueAt.Equal(time.Date(2026, 10, 26, 11, 0, 0, 0, v.f.Loc())) {
		t.Fatalf("step 3 due %v, want Mon 11:00", j.DueAt)
	}
	v.at("2026-10-26 11:00") // step 3 due but no tracking -> held, not sent
	if n := len(v.c.msgs); n != 3 {
		t.Fatalf("step 3 must not send without tracking; msgs=%d", n)
	}
	if j := v.job(o.ID, "3"); !j.Held || j.Status != store.JobPending {
		t.Fatalf("step 3 should be held pending: %+v", j)
	}

	if err := v.e.Dispatched(ctx, o.ID, "LP123", "YunExpress", "", v.now); err != nil {
		t.Fatal(err)
	}
	v.at("2026-10-26 11:01")
	last := v.c.msgs[len(v.c.msgs)-1]
	if !strings.Contains(last.Subject, "has shipped") || !strings.Contains(last.HTML, "LP123") {
		t.Fatalf("step 3 not sent with tracking: %q", last.Subject)
	}

	// Carrier reports movement before step 4 fires -> 4 skipped, chain continues to 5.
	if err := v.e.TrackingEvent(ctx, o.ID, flow.EvInTransit, "Departed facility", "api", v.now); err != nil {
		t.Fatal(err)
	}
	v.at("2026-11-02 12:00") // well past 90 business hours
	if j := v.job(o.ID, "4"); j.Status != store.JobSkipped {
		t.Fatalf("step 4 should be skipped (carrier moved): %+v", j)
	}
	if j := v.job(o.ID, "5"); j.ID == 0 {
		t.Fatal("step 5 must be scheduled after skipped step 4")
	}

	// Arrival -> step 6 now; step 5 (not_arrived) later gets skipped.
	if err := v.e.TrackingEvent(ctx, o.ID, flow.EvArrivedCountry, "Arrived at destination", "api", v.now); err != nil {
		t.Fatal(err)
	}
	v.at("2026-11-02 12:01")
	if last := v.c.msgs[len(v.c.msgs)-1]; !strings.Contains(last.Subject, "arrived in your country") {
		t.Fatalf("step 6 not sent: %q", last.Subject)
	}
	v.at("2026-11-05 12:00")
	if j := v.job(o.ID, "5"); j.Status != store.JobSkipped {
		t.Fatalf("step 5 should be skipped after arrival: %+v", j)
	}
	if err := v.e.TrackingEvent(ctx, o.ID, flow.EvOutForDelivery, "", "api", v.now); err != nil {
		t.Fatal(err)
	}
	v.at("2026-11-05 12:01")
	if last := v.c.msgs[len(v.c.msgs)-1]; !strings.Contains(last.Subject, "out for delivery") {
		t.Fatalf("step 7 not sent: %q", last.Subject)
	}
	// every email sent exactly once
	seen := map[string]bool{}
	for _, m := range v.c.msgs {
		if seen[m.MessageID] {
			t.Fatalf("duplicate message %s", m.MessageID)
		}
		seen[m.MessageID] = true
	}
}

func TestStep4SendsWhenTrackingStaysSilent(t *testing.T) {
	v := setup(t)
	o := v.order()
	v.at("2026-10-21 11:00")
	v.at("2026-10-22 11:00")
	v.at("2026-10-23 11:00")
	_ = v.e.Dispatched(context.Background(), o.ID, "LP1", "", "", v.now)
	v.at("2026-10-26 11:00") // step 3 sent
	// 90 business h after Mon 11:00 = Fri 05:00 -> inside no window -> Fri 08:00
	if j := v.job(o.ID, "4"); !j.DueAt.Equal(time.Date(2026, 10, 30, 8, 0, 0, 0, v.f.Loc())) {
		t.Fatalf("step 4 due %v, want Fri 08:00", j.DueAt)
	}
	v.at("2026-10-30 08:00")
	if last := v.c.msgs[len(v.c.msgs)-1]; !strings.Contains(last.Subject, "hasn't moved yet") {
		t.Fatalf("step 4 not sent: %v", v.c.subjects())
	}
}

func TestIdempotentOrderPaid(t *testing.T) {
	v := setup(t)
	v.order()
	o2 := &store.Order{StoreID: "arman-curren", ExternalID: "9001", Name: "#1001", Email: "buyer@example.com", PaidAt: v.now}
	created, err := v.e.OrderPaid(context.Background(), o2)
	if err != nil || created {
		t.Fatalf("redelivered webhook must be a no-op: created=%v err=%v", created, err)
	}
	v.at("2026-10-21 11:00")
	if len(v.c.msgs) != 1 {
		t.Fatalf("msgs=%d want 1", len(v.c.msgs))
	}
}

func TestCancelStopsEverything(t *testing.T) {
	v := setup(t)
	o := v.order()
	_ = v.e.CancelOrder(context.Background(), o.ID, "refunded")
	v.at("2026-10-23 12:00")
	if len(v.c.msgs) != 0 {
		t.Fatalf("canceled order emailed: %v", v.c.subjects())
	}
}

func TestStepOffSkipsButKeepsChain(t *testing.T) {
	v := setup(t)
	o := v.order()
	_ = v.s.SetStepOverride(context.Background(), "arman-curren", "2", flow.StatusOff)
	v.at("2026-10-21 11:00")
	v.at("2026-10-22 11:00")
	if j := v.job(o.ID, "2"); j.Status != store.JobSkipped {
		t.Fatalf("step 2: %+v", j)
	}
	if j := v.job(o.ID, "2.5"); j.ID == 0 {
		t.Fatal("2.5 must still be scheduled when 2 is paused")
	}
}

func TestSendFailureRetriesOnlyWhenNothingWasSent(t *testing.T) {
	v := setup(t)
	o := v.order()
	v.c.fail = errors.New("smtp commit: connection reset") // ambiguous: must not auto-retry
	v.at("2026-10-21 11:00")
	if j := v.job(o.ID, "1"); j.Status != store.JobFailed {
		t.Fatalf("ambiguous failure must stop for a human: %+v", j)
	}
	v2 := setup(t)
	o2 := v2.order()
	v2.c.fail = errors.Join(mail.ErrNotSent, errors.New("dial: refused"))
	v2.at("2026-10-21 11:00")
	if j := v2.job(o2.ID, "1"); j.Status != store.JobPending || j.Attempts != 1 {
		t.Fatalf("pre-DATA failure should retry later: %+v", j)
	}
}

func TestSendWindowDefersNightEmails(t *testing.T) {
	v := setup(t)
	v.now = time.Date(2026, 10, 21, 23, 30, 0, 0, v.f.Loc())
	o := v.order()
	// step 2: +24h exact = Thu 23:30 -> outside window -> Fri 08:00
	if j := v.job(o.ID, "2"); !j.DueAt.Equal(time.Date(2026, 10, 23, 8, 0, 0, 0, v.f.Loc())) {
		t.Fatalf("step 2 due %v", j.DueAt)
	}
	// step 1 is immediate: ignores the window.
	if j := v.job(o.ID, "1"); !j.DueAt.Equal(v.now) {
		t.Fatalf("step 1 due %v", j.DueAt)
	}
}
