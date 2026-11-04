package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/icl00ud/mailflow/internal/auth"
	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/store"
)

// seedDemo builds a deterministic-looking tenant in the in-memory store so the
// panel can be reviewed (and screenshotted) without Shopify or Postgres. It
// also mints a one-time invite for demo@mailflow.local.
func seedDemo(ctx context.Context, st store.Store, eng *engine.Engine, origin string) (string, error) {
	const tenantID = "arman-curren"
	const email = "demo@mailflow.local"
	now := eng.Now()

	u, err := st.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		u, err = st.CreateUser(ctx, email)
	}
	if err != nil {
		return "", fmt.Errorf("demo user: %w", err)
	}
	if err := st.AddMembership(ctx, u.ID, tenantID, store.RoleOwner); err != nil {
		return "", fmt.Errorf("demo membership: %w", err)
	}

	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	inv := &store.Invite{ID: store.NewID(), TenantID: tenantID, Email: u.Email, Role: store.RoleOwner,
		TokenHash: auth.HashToken(token), ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now}
	if err := st.CreateInvite(ctx, inv); err != nil {
		return "", fmt.Errorf("demo invite: %w", err)
	}

	order := func(name, customer, country string, paid time.Time, tracking string) (*store.Order, error) {
		o := &store.Order{
			StoreID: tenantID, ExternalID: "demo-" + name, Name: name, Email: "cliente+" + name[1:] + "@exemplo.com",
			CustomerName: customer, Currency: "USD", Total: "$104.99", Country: country, PaidAt: paid,
			Items: []store.Item{{Title: "Azure Chronograph Leather", Variant: "Blue / Brown leather", Qty: 1}},
		}
		if _, err := eng.OrderPaid(ctx, o); err != nil {
			return nil, err
		}
		if tracking != "" {
			if err := eng.Dispatched(ctx, o.ID, tracking, "YunExpress", "https://parcelsapp.com/en/tracking/"+tracking, paid.Add(70*time.Hour)); err != nil {
				return nil, err
			}
		}
		return o, nil
	}

	ensure := func(o *store.Order, step string, due time.Time) (*store.Job, error) {
		if _, err := st.Schedule(ctx, &store.Job{OrderID: o.ID, StoreID: o.StoreID, StepID: step, DueAt: due}); err != nil {
			return nil, err
		}
		jobs, err := st.OrderJobs(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		for i := range jobs {
			if jobs[i].StepID == step {
				return &jobs[i], nil
			}
		}
		return nil, fmt.Errorf("demo: step %s not scheduled", step)
	}
	sent := func(o *store.Order, step string, at time.Time) error {
		j, err := ensure(o, step, at)
		if err != nil {
			return err
		}
		t := at
		return st.Finish(ctx, j.ID, store.JobSent, "", fmt.Sprintf("demo-%d-%s@armancurren.com", o.ID, step), &t)
	}
	skipped := func(o *store.Order, step, reason string, at time.Time) error {
		j, err := ensure(o, step, at)
		if err != nil {
			return err
		}
		return st.Finish(ctx, j.ID, store.JobSkipped, reason, "", nil)
	}
	failed := func(o *store.Order, step, reason string, at time.Time) error {
		j, err := ensure(o, step, at)
		if err != nil {
			return err
		}
		return st.Finish(ctx, j.ID, store.JobFailed, reason, "", nil)
	}
	held := func(o *store.Order, step string, due time.Time) error {
		j, err := ensure(o, step, due)
		if err != nil {
			return err
		}
		return st.Defer(ctx, j.ID, due, true, "waiting for a real tracking number")
	}
	event := func(o *store.Order, name, label, source string, at time.Time) error {
		_, err := st.AddEvent(ctx, &store.Event{OrderID: o.ID, Event: name, Label: label, Source: source, At: at})
		return err
	}

	// #1042 — paid four days ago, in transit, step 4 scheduled.
	o1, err := order("#1042", "Alex Morgan", "US", now.Add(-4*24*time.Hour), "LP00123456789012")
	if err != nil {
		return "", err
	}
	for _, s := range []struct {
		step string
		at   time.Time
	}{{"1", o1.PaidAt.Add(time.Minute)}, {"2", o1.PaidAt.Add(24 * time.Hour)}, {"2.5", o1.PaidAt.Add(49 * time.Hour)}, {"3", o1.PaidAt.Add(74 * time.Hour)}} {
		if err := sent(o1, s.step, s.at); err != nil {
			return "", err
		}
	}
	if _, err := ensure(o1, "4", now.Add(30*time.Hour)); err != nil {
		return "", err
	}
	if err := event(o1, "in_transit", "Departed from origin facility", "17track", o1.PaidAt.Add(80*time.Hour)); err != nil {
		return "", err
	}

	// #1043 — waiting for a tracking number: step 2.5 scheduled, step 3 held.
	o2, err := order("#1043", "Marina Costa", "BR", now.Add(-28*time.Hour), "")
	if err != nil {
		return "", err
	}
	if err := sent(o2, "1", o2.PaidAt.Add(time.Minute)); err != nil {
		return "", err
	}
	if err := sent(o2, "2", o2.PaidAt.Add(24*time.Hour)); err != nil {
		return "", err
	}
	if _, err := ensure(o2, "2.5", now.Add(20*time.Hour)); err != nil {
		return "", err
	}
	if err := held(o2, "3", now.Add(-2*time.Hour)); err != nil {
		return "", err
	}

	// #1044 — arrived in country, step 4 skipped, tracking lane active.
	o3, err := order("#1044", "Jonas Weber", "DE", now.Add(-8*24*time.Hour), "LP00987654321098")
	if err != nil {
		return "", err
	}
	for _, s := range []struct {
		step string
		at   time.Time
	}{{"1", o3.PaidAt.Add(time.Minute)}, {"2", o3.PaidAt.Add(24 * time.Hour)}, {"2.5", o3.PaidAt.Add(50 * time.Hour)}, {"3", o3.PaidAt.Add(75 * time.Hour)}} {
		if err := sent(o3, s.step, s.at); err != nil {
			return "", err
		}
	}
	if err := skipped(o3, "4", "carrier already reported movement", o3.PaidAt.Add(96*time.Hour)); err != nil {
		return "", err
	}
	if err := sent(o3, "5", o3.PaidAt.Add(120*time.Hour)); err != nil {
		return "", err
	}
	if err := event(o3, "in_transit", "In transit to destination country", "17track", o3.PaidAt.Add(90*time.Hour)); err != nil {
		return "", err
	}
	if err := event(o3, "arrived_country", "Arrived at destination country", "17track", now.Add(-20*time.Hour)); err != nil {
		return "", err
	}
	if err := sent(o3, "6", now.Add(-19*time.Hour)); err != nil {
		return "", err
	}
	if _, err := ensure(o3, "7", now.Add(8*time.Hour)); err != nil {
		return "", err
	}

	// #1045 — one failed send that needs a human retry.
	o4, err := order("#1045", "Sofia Rossi", "IT", now.Add(-6*24*time.Hour), "LP00445566778899")
	if err != nil {
		return "", err
	}
	for _, s := range []struct {
		step string
		at   time.Time
	}{{"1", o4.PaidAt.Add(time.Minute)}, {"2", o4.PaidAt.Add(24 * time.Hour)}, {"2.5", o4.PaidAt.Add(49 * time.Hour)}, {"3", o4.PaidAt.Add(74 * time.Hour)}} {
		if err := sent(o4, s.step, s.at); err != nil {
			return "", err
		}
	}
	if err := failed(o4, "4", "SMTP 421 timeout while sending; verify the inbox before retrying", now.Add(-3*time.Hour)); err != nil {
		return "", err
	}

	// #1046 — canceled journey.
	o5, err := order("#1046", "Liam O'Connor", "IE", now.Add(-12*24*time.Hour), "")
	if err != nil {
		return "", err
	}
	if err := sent(o5, "1", o5.PaidAt.Add(time.Minute)); err != nil {
		return "", err
	}
	if err := eng.CancelOrder(ctx, o5.ID, "refunded before dispatch"); err != nil {
		return "", err
	}

	// #1047 — a fresh order, first email sent, next one scheduled.
	o6, err := order("#1047", "Yuki Tanaka", "JP", now.Add(-3*time.Hour), "")
	if err != nil {
		return "", err
	}
	if err := sent(o6, "1", o6.PaidAt.Add(time.Minute)); err != nil {
		return "", err
	}
	if _, err := ensure(o6, "2", now.Add(21*time.Hour)); err != nil {
		return "", err
	}

	return origin + "/invite/" + token, nil
}
