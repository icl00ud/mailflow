package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// The same behavioural contract runs against Memory and, when TEST_DATABASE_URL
// is set, against a real Postgres.
func TestMemoryContract(t *testing.T) {
	m := NewMemory()
	contract(t, m)
	authContract(t, NewMemory())
}

func TestPostgresContract(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pg, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if _, err := pg.pool.Exec(ctx, `TRUNCATE orders, step_overrides, tenants, users, webauthn_challenges RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	contract(t, pg)
	authContract(t, pg)
	// schema is idempotent: a second boot must not fail
	pg2, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}
	pg2.Close()
}

func contract(t *testing.T, s Store) {
	ctx := context.Background()
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	o := &Order{StoreID: "s", ExternalID: "e1", Name: "#1", Email: "a@b.co", PaidAt: now, Items: []Item{{Title: "W", Qty: 2}}}
	if c, err := s.UpsertOrder(ctx, o); err != nil || !c || o.ID == 0 {
		t.Fatalf("upsert: created=%v id=%d err=%v", c, o.ID, err)
	}
	dup := &Order{StoreID: "s", ExternalID: "e1", Email: "a@b.co", PaidAt: now}
	if c, err := s.UpsertOrder(ctx, dup); err != nil || c || dup.ID != o.ID {
		t.Fatalf("duplicate upsert: created=%v id=%d want %d err=%v", c, dup.ID, o.ID, err)
	}
	got, err := s.OrderByExternal(ctx, "s", "e1")
	if err != nil || got.ID != o.ID || len(got.Items) != 1 || got.Items[0].Qty != 2 {
		t.Fatalf("by external: %+v %v", got, err)
	}
	if _, err := s.OrderByID(ctx, 999999); err != ErrNotFound {
		t.Fatalf("missing order err=%v", err)
	}

	// tracking
	if ch, err := s.SetTracking(ctx, o.ID, "LP1", "Yun", "", now); err != nil || !ch {
		t.Fatalf("first tracking: changed=%v err=%v", ch, err)
	}
	if ch, _ := s.SetTracking(ctx, o.ID, "LP1", "Yun", "", now.Add(time.Hour)); ch {
		t.Fatal("same number must not report changed")
	}
	got, _ = s.OrderByID(ctx, o.ID)
	if got.Tracking != "LP1" || got.DispatchedAt == nil || !got.DispatchedAt.Equal(now) {
		t.Fatalf("dispatch time must stick to first attach: %+v", got)
	}

	// events: once per (order, event)
	if c, _ := s.AddEvent(ctx, &Event{OrderID: o.ID, Event: "in_transit", At: now}); !c {
		t.Fatal("first event not created")
	}
	if c, _ := s.AddEvent(ctx, &Event{OrderID: o.ID, Event: "in_transit", At: now}); c {
		t.Fatal("duplicate event created")
	}

	// jobs
	j1 := &Job{OrderID: o.ID, StoreID: "s", StepID: "1", DueAt: now}
	j2 := &Job{OrderID: o.ID, StoreID: "s", StepID: "2", DueAt: now.Add(24 * time.Hour)}
	for _, j := range []*Job{j1, j2} {
		if c, err := s.Schedule(ctx, j); err != nil || !c {
			t.Fatalf("schedule: %v %v", c, err)
		}
	}
	if c, _ := s.Schedule(ctx, &Job{OrderID: o.ID, StoreID: "s", StepID: "1", DueAt: now}); c {
		t.Fatal("job scheduled twice")
	}
	claimed, err := s.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(claimed) != 1 || claimed[0].StepID != "1" || claimed[0].Status != JobSending || claimed[0].Attempts != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if again, _ := s.ClaimDue(ctx, now.Add(time.Minute), 10); len(again) != 0 {
		t.Fatal("a claimed job was claimed twice")
	}
	if err := s.Defer(ctx, claimed[0].ID, now.Add(time.Hour), true, "waiting"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.ClaimDue(ctx, now.Add(2*time.Minute), 10); len(n) != 0 {
		t.Fatal("deferred job came back early")
	}
	if err := s.WakeHeld(ctx, o.ID, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, _ := s.ClaimDue(ctx, now.Add(4*time.Minute), 10)
	if len(n) != 1 || n[0].StepID != "1" || n[0].Attempts != 2 {
		t.Fatalf("woken job: %+v", n)
	}
	sent := now.Add(5 * time.Minute)
	if err := s.Finish(ctx, n[0].ID, JobSent, "", "<m@x>", &sent); err != nil {
		t.Fatal(err)
	}
	jb, _ := s.JobByID(ctx, n[0].ID)
	if jb.Status != JobSent || jb.SentAt == nil || jb.MessageID != "<m@x>" {
		t.Fatalf("finished job: %+v", jb)
	}
	counts, _ := s.StepCounts(ctx, "s")
	if counts["1"][JobSent] != 1 || counts["2"][JobPending] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	if err := s.CancelPending(ctx, o.ID, "refund"); err != nil {
		t.Fatal(err)
	}
	if jb, _ := s.JobByID(ctx, j2.ID); jb.Status != JobCanceled {
		t.Fatalf("cancel pending: %+v", jb)
	}
	if err := s.Retry(ctx, j2.ID, now); err != nil { // canceled jobs are final
		t.Fatal(err)
	}
	if jb, _ := s.JobByID(ctx, j2.ID); jb.Status != JobCanceled {
		t.Fatal("retry must not revive canceled jobs")
	}

	// stuck sending
	j3 := &Job{OrderID: o.ID, StoreID: "s", StepID: "3", DueAt: now}
	_, _ = s.Schedule(ctx, j3)
	_, _ = s.ClaimDue(ctx, now, 10)
	if n, err := s.FailStuck(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("fail stuck: %d %v", n, err)
	}
	if jb, _ := s.JobByID(ctx, j3.ID); jb.Status != JobFailed {
		t.Fatalf("stuck job: %+v", jb)
	}
	if err := s.Retry(ctx, j3.ID, now); err != nil {
		t.Fatal(err)
	}
	if jb, _ := s.JobByID(ctx, j3.ID); jb.Status != JobPending {
		t.Fatalf("retry failed job: %+v", jb)
	}

	// overrides
	_ = s.SetStepOverride(ctx, "s", "2", "off")
	_ = s.SetStepOverride(ctx, "s", "2", "on")
	if ov, _ := s.StepOverrides(ctx, "s"); ov["2"] != "on" {
		t.Fatalf("override: %v", ov)
	}
	if orders, err := s.ListOrders(ctx, "s", 10); err != nil || len(orders) != 1 {
		t.Fatalf("list: %v %v", orders, err)
	}
}
