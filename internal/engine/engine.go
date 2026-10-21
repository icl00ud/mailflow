// Package engine turns orders and carrier events into scheduled emails and sends them.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/flow"
	mailpkg "github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/store"
)

const (
	maxSendAttempts = 3
	holdRecheck     = 5 * time.Minute
)

// Tracker subscribes a parcel to carrier tracking updates (17TRACK).
type Tracker interface {
	Register(ctx context.Context, number, tag string) error
}

type Engine struct {
	Tracker Tracker // optional
	Store   store.Store
	Flows   map[string]*flow.Flow
	Sender  mailpkg.Sender
	Render  *mailpkg.Renderer
	Log     *slog.Logger
	Now     func() time.Time
}

func New(s store.Store, flows map[string]*flow.Flow, sender mailpkg.Sender, log *slog.Logger) *Engine {
	return &Engine{Store: s, Flows: flows, Sender: sender, Render: mailpkg.NewRenderer(), Log: log, Now: time.Now}
}

func (e *Engine) flow(storeID string) (*flow.Flow, error) {
	f, ok := e.Flows[storeID]
	if !ok {
		return nil, fmt.Errorf("unknown store %q", storeID)
	}
	return f, nil
}

// OrderPaid registers a paid order and schedules the steps that start at payment.
// It is idempotent: Shopify redelivers webhooks.
func (e *Engine) OrderPaid(ctx context.Context, o *store.Order) (bool, error) {
	f, err := e.flow(o.StoreID)
	if err != nil {
		return false, err
	}
	if _, err := mail.ParseAddress(o.Email); err != nil {
		return false, fmt.Errorf("order %s has no valid email: %w", o.ExternalID, err)
	}
	created, err := e.Store.UpsertOrder(ctx, o)
	if err != nil || !created {
		return false, err
	}
	for _, s := range f.ByTrigger(flow.TriggerOrderPaid) {
		if s.Status == flow.StatusDraft {
			continue
		}
		if err := e.schedule(ctx, f, o, s, o.PaidAt); err != nil {
			return true, err
		}
	}
	e.Log.Info("order registered", "store", o.StoreID, "order", o.Name, "id", o.ID)
	return true, nil
}

func (e *Engine) schedule(ctx context.Context, f *flow.Flow, o *store.Order, s flow.Step, anchor time.Time) error {
	j := &store.Job{OrderID: o.ID, StoreID: f.Store.ID, StepID: s.ID, DueAt: f.DueAt(s, anchor)}
	created, err := e.Store.Schedule(ctx, j)
	if err == nil && created {
		e.Log.Info("scheduled", "order", o.Name, "step", s.ID, "due", j.DueAt.In(f.Loc()).Format(time.RFC3339))
	}
	return err
}

// release schedules the steps that chain "after_step: parent".
func (e *Engine) release(ctx context.Context, f *flow.Flow, o *store.Order, parent string, anchor time.Time) error {
	for _, d := range f.Dependents(parent) {
		if d.Status == flow.StatusDraft {
			continue
		}
		if err := e.schedule(ctx, f, o, d, anchor); err != nil {
			return err
		}
	}
	return nil
}

// Dispatched attaches the real tracking number (from Shopify's fulfillment, or the panel).
func (e *Engine) Dispatched(ctx context.Context, orderID int64, number, carrier, url string, at time.Time) error {
	if strings.TrimSpace(number) == "" {
		return errors.New("empty tracking number")
	}
	changed, err := e.Store.SetTracking(ctx, orderID, number, carrier, url, at)
	if err != nil {
		return err
	}
	if changed {
		if e.Tracker != nil {
			// A failure must not block the shipped email; steps 6/7 then need the panel's "Marcar".
			rc, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := e.Tracker.Register(rc, number, fmt.Sprintf("mf:%d", orderID)); err != nil {
				e.Log.Error("tracking provider registration failed", "order", orderID, "number", number, "err", err)
			}
			cancel()
		}
		return e.Store.WakeHeld(ctx, orderID, e.Now())
	}
	return nil
}

// TrackingEvent records a carrier milestone and schedules steps bound to it.
func (e *Engine) TrackingEvent(ctx context.Context, orderID int64, ev, label, source string, at time.Time) error {
	o, err := e.Store.OrderByID(ctx, orderID)
	if err != nil {
		return err
	}
	f, err := e.flow(o.StoreID)
	if err != nil {
		return err
	}
	if at.IsZero() {
		at = e.Now()
	}
	created, err := e.Store.AddEvent(ctx, &store.Event{OrderID: orderID, Event: ev, Label: label, Source: source, At: at})
	if err != nil || !created {
		return err
	}
	if ev == flow.EvDelivered {
		if err := e.Store.CancelPending(ctx, orderID, "package delivered"); err != nil {
			return err
		}
	}
	for _, s := range f.ByEvent(ev) {
		if s.Status == flow.StatusDraft {
			continue
		}
		if err := e.schedule(ctx, f, o, s, e.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) CancelOrder(ctx context.Context, orderID int64, reason string) error {
	if err := e.Store.CancelOrder(ctx, orderID); err != nil {
		return err
	}
	return e.Store.CancelPending(ctx, orderID, reason)
}

// Run processes due jobs until ctx ends.
func (e *Engine) Run(ctx context.Context, every time.Duration) {
	if n, err := e.Store.FailStuck(ctx, e.Now().Add(-10*time.Minute)); err != nil {
		e.Log.Error("fail-stuck", "err", err)
	} else if n > 0 {
		e.Log.Warn("jobs left in 'sending' marked failed; verify before retrying", "n", n)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := e.Tick(ctx); err != nil {
			e.Log.Error("tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick claims and processes every due job once.
func (e *Engine) Tick(ctx context.Context) error {
	jobs, err := e.Store.ClaimDue(ctx, e.Now(), 50)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := e.process(ctx, j); err != nil {
			e.Log.Error("process job", "job", j.ID, "step", j.StepID, "err", err)
		}
	}
	return nil
}

type flags struct{ movement, arrived, delivered bool }

func eventFlags(evs []store.Event) flags {
	var fl flags
	for _, ev := range evs {
		fl.movement = true
		switch ev.Event {
		case flow.EvArrivedCountry, flow.EvOutForDelivery, flow.EvReadyForPickup:
			fl.arrived = true
		case flow.EvDelivered:
			fl.arrived, fl.delivered = true, true
		}
	}
	return fl
}

func (e *Engine) skip(ctx context.Context, f *flow.Flow, o *store.Order, j store.Job, reason string) error {
	e.Log.Info("skipped", "order", o.Name, "step", j.StepID, "reason", reason)
	if err := e.Store.Finish(ctx, j.ID, store.JobSkipped, reason, "", nil); err != nil {
		return err
	}
	return e.release(ctx, f, o, j.StepID, j.DueAt) // a skipped step does not freeze the chain
}

func (e *Engine) process(ctx context.Context, j store.Job) error {
	o, err := e.Store.OrderByID(ctx, j.OrderID)
	if err != nil {
		return err
	}
	f, err := e.flow(j.StoreID)
	if err != nil {
		return e.Store.Finish(ctx, j.ID, store.JobFailed, err.Error(), "", nil)
	}
	step, ok := f.Step(j.StepID)
	if !ok {
		return e.Store.Finish(ctx, j.ID, store.JobFailed, "step no longer exists in flow", "", nil)
	}
	if o.Canceled {
		return e.Store.Finish(ctx, j.ID, store.JobCanceled, "order canceled", "", nil)
	}
	over, err := e.Store.StepOverrides(ctx, j.StoreID)
	if err != nil {
		return err
	}
	if !step.Sends(over) {
		return e.skip(ctx, f, o, j, "step is off")
	}

	evs, err := e.Store.Events(ctx, o.ID)
	if err != nil {
		return err
	}
	fl := eventFlags(evs)
	now := e.Now()
	hold := ""
	for _, c := range step.Conditions {
		switch c {
		case flow.CondNoCarrierMove:
			if fl.movement {
				return e.skip(ctx, f, o, j, "carrier already reported movement")
			}
		case flow.CondNotArrived:
			if fl.arrived {
				return e.skip(ctx, f, o, j, "parcel already reached the destination country")
			}
		case flow.CondNotDelivered:
			if fl.delivered {
				return e.skip(ctx, f, o, j, "already delivered")
			}
		case flow.CondWithinWindow:
			base := o.PaidAt
			if o.DispatchedAt != nil {
				base = *o.DispatchedAt
			}
			if now.Sub(base) > time.Duration(step.WindowDays)*24*time.Hour {
				return e.skip(ctx, f, o, j, fmt.Sprintf("past the %d-day window", step.WindowDays))
			}
		case flow.CondHasTracking:
			if o.Tracking == "" {
				hold = "waiting for a real tracking number"
			}
		}
	}
	if hold != "" {
		return e.Store.Defer(ctx, j.ID, now.Add(holdRecheck), true, hold)
	}

	msg, err := e.build(f, step, o)
	if err != nil {
		return e.Store.Finish(ctx, j.ID, store.JobFailed, "render: "+err.Error(), "", nil)
	}
	if err := e.Sender.Send(ctx, msg); err != nil {
		if errors.Is(err, mailpkg.ErrNotSent) && j.Attempts < maxSendAttempts {
			backoff := time.Duration(j.Attempts*j.Attempts) * 5 * time.Minute
			e.Log.Warn("send failed, will retry", "order", o.Name, "step", step.ID, "err", err, "in", backoff)
			return e.Store.Defer(ctx, j.ID, now.Add(backoff), false, err.Error())
		}
		e.Log.Error("send failed", "order", o.Name, "step", step.ID, "err", err)
		return e.Store.Finish(ctx, j.ID, store.JobFailed, err.Error(), "", nil)
	}
	sent := e.Now()
	e.Log.Info("sent", "order", o.Name, "step", step.ID, "to", o.Email)
	if err := e.Store.Finish(ctx, j.ID, store.JobSent, "", msg.MessageID, &sent); err != nil {
		return err
	}
	return e.release(ctx, f, o, step.ID, sent)
}

func domainOf(addr string) string {
	if a, err := mail.ParseAddress(addr); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			return a.Address[i+1:]
		}
	}
	return "localhost"
}

func (e *Engine) build(f *flow.Flow, step flow.Step, o *store.Order) (mailpkg.Message, error) {
	first := strings.Fields(o.CustomerName)
	name := "there"
	if len(first) > 0 {
		name = first[0]
	}
	subject, htmlBody, text, err := e.Render.Render(f, step, mailpkg.Data{
		Store: f.Store, Step: step, Order: *o, FirstName: name, Items: o.Items,
		Tracking: o.Tracking, Carrier: o.Carrier, TrackURL: trackURL(o),
	})
	if err != nil {
		return mailpkg.Message{}, err
	}
	return mailpkg.Message{
		From: f.Store.From, To: o.Email, ReplyTo: f.Store.ReplyTo,
		Subject: subject, HTML: htmlBody, Text: text,
		MessageID: fmt.Sprintf("mf-%s-%d-%s@%s", f.Store.ID, o.ID, step.ID, domainOf(f.Store.From)),
	}, nil
}

func trackURL(o *store.Order) string {
	if o.TrackingURL != "" {
		return o.TrackingURL
	}
	if o.Tracking != "" {
		return "https://parcelsapp.com/en/tracking/" + o.Tracking
	}
	return ""
}

// SampleOrder is used by previews and test sends.
func SampleOrder(f *flow.Flow, now time.Time) *store.Order {
	return &store.Order{
		ID: 0, StoreID: f.Store.ID, ExternalID: "preview", Name: "#1042", Email: "preview@example.com",
		CustomerName: "Alex Morgan", Currency: "USD", Total: "$194.98", Country: "US", PaidAt: now.Add(-24 * time.Hour),
		Items: []store.Item{{Title: "Azure Chronograph Leather", Variant: "Blue / Brown leather", Qty: 1,
			Image: f.Store.Site + "/curren/multifunctional-chronograph-new-watch-cur139/card.webp"}},
		Tracking: "LP00123456789012", Carrier: "YunExpress", TrackingURL: "https://parcelsapp.com/en/tracking/LP00123456789012",
	}
}

// Preview renders a step with sample data.
func (e *Engine) Preview(storeID, stepID string) (subject, htmlBody string, err error) {
	f, err := e.flow(storeID)
	if err != nil {
		return "", "", err
	}
	step, ok := f.Step(stepID)
	if !ok {
		return "", "", fmt.Errorf("unknown step %q", stepID)
	}
	if step.Status == flow.StatusDraft {
		return "", "", errors.New("step is a draft: no copy written yet")
	}
	msg, err := e.build(f, step, SampleOrder(f, e.Now()))
	return msg.Subject, msg.HTML, err
}

// TestSend emails a step rendered with sample data to a chosen address.
func (e *Engine) TestSend(ctx context.Context, storeID, stepID, to string) error {
	f, err := e.flow(storeID)
	if err != nil {
		return err
	}
	step, ok := f.Step(stepID)
	if !ok || step.Status == flow.StatusDraft {
		return errors.New("unknown or draft step")
	}
	o := SampleOrder(f, e.Now())
	o.Email = to
	msg, err := e.build(f, step, o)
	if err != nil {
		return err
	}
	msg.Subject = "[TEST] " + msg.Subject
	msg.MessageID = fmt.Sprintf("mf-test-%d@%s", e.Now().UnixNano(), domainOf(f.Store.From))
	return e.Sender.Send(ctx, msg)
}
