// Package store persists orders, scheduled email jobs and carrier events.
package store

import (
	"context"
	"errors"
	"time"
)

const (
	JobPending  = "pending"
	JobSending  = "sending"
	JobSent     = "sent"
	JobSkipped  = "skipped"
	JobFailed   = "failed"
	JobCanceled = "canceled"
)

var ErrNotFound = errors.New("not found")

type Item struct {
	Title   string `json:"title"`
	Variant string `json:"variant,omitempty"`
	Image   string `json:"image,omitempty"`
	// VariantID is the Shopify variant id; used to look up the photo (shopify.FillImages).
	VariantID int64 `json:"variant_id,omitempty"`
	Qty       int   `json:"qty"`
}

type Order struct {
	ID           int64      `json:"id"`
	StoreID      string     `json:"store_id"`
	ExternalID   string     `json:"external_id"` // Shopify order id
	Name         string     `json:"name"`        // "#1001"
	Email        string     `json:"email"`
	CustomerName string     `json:"customer_name"`
	Currency     string     `json:"currency"`
	Total        string     `json:"total"`
	Country      string     `json:"country"`
	Items        []Item     `json:"items"`
	PaidAt       time.Time  `json:"paid_at"`
	Tracking     string     `json:"tracking_number,omitempty"`
	Carrier      string     `json:"carrier,omitempty"`
	TrackingURL  string     `json:"tracking_url,omitempty"`
	DispatchedAt *time.Time `json:"dispatched_at,omitempty"` // when a real tracking number was attached
	Canceled     bool       `json:"canceled"`
	CreatedAt    time.Time  `json:"created_at"`
}

type Job struct {
	ID        int64      `json:"id"`
	OrderID   int64      `json:"order_id"`
	StoreID   string     `json:"store_id"`
	StepID    string     `json:"step_id"`
	DueAt     time.Time  `json:"due_at"`
	Status    string     `json:"status"`
	Held      bool       `json:"held"` // due, but waiting on a hold condition (real tracking number)
	Attempts  int        `json:"attempts"`
	Reason    string     `json:"reason,omitempty"` // skip/fail explanation
	MessageID string     `json:"message_id,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
}

type Event struct {
	ID      int64     `json:"id"`
	OrderID int64     `json:"order_id"`
	Event   string    `json:"event"`  // normalised flow.Ev*
	Label   string    `json:"label"`  // raw carrier text
	Source  string    `json:"source"` // shopify | api | panel
	At      time.Time `json:"at"`
}

type StepCounts map[string]map[string]int // stepID -> status -> n

type Store interface {
	AuthStore

	Ping(ctx context.Context) error

	// UpsertOrder inserts by (store, external id); on conflict it leaves the row alone and sets o.ID.
	UpsertOrder(ctx context.Context, o *Order) (created bool, err error)
	OrderByID(ctx context.Context, id int64) (*Order, error)
	OrderByExternal(ctx context.Context, storeID, externalID string) (*Order, error)
	ListOrders(ctx context.Context, storeID string, limit int) ([]Order, error)
	CancelOrder(ctx context.Context, id int64) error
	// SetTracking attaches a real tracking number. changed is true the first time or when the number changes.
	SetTracking(ctx context.Context, id int64, number, carrier, url string, at time.Time) (changed bool, err error)

	// AddEvent records a carrier event once per (order, event).
	AddEvent(ctx context.Context, e *Event) (created bool, err error)
	Events(ctx context.Context, orderID int64) ([]Event, error)

	// Schedule inserts a job once per (order, step). created=false when it already existed.
	Schedule(ctx context.Context, j *Job) (created bool, err error)
	// ClaimDue atomically flips due pending jobs to sending.
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]Job, error)
	Finish(ctx context.Context, id int64, status, reason, messageID string, sentAt *time.Time) error
	// Defer returns a claimed job to pending with a new due time.
	Defer(ctx context.Context, id int64, until time.Time, held bool, reason string) error
	// WakeHeld makes held jobs of an order due immediately.
	WakeHeld(ctx context.Context, orderID int64, now time.Time) error
	// Retry puts a failed/skipped job back to pending, due now.
	Retry(ctx context.Context, id int64, now time.Time) error
	// CancelPending cancels every pending job of an order (order canceled/refunded).
	CancelPending(ctx context.Context, orderID int64, reason string) error
	// FailStuck marks jobs left in 'sending' since before t as failed: a human must verify before resending.
	FailStuck(ctx context.Context, before time.Time) (int, error)
	JobByID(ctx context.Context, id int64) (*Job, error)
	OrderJobs(ctx context.Context, orderID int64) ([]Job, error)
	StepCounts(ctx context.Context, storeID string) (StepCounts, error)
	// StepHeldCounts counts pending jobs that are held per step ("retido").
	StepHeldCounts(ctx context.Context, storeID string) (map[string]int, error)

	StepOverrides(ctx context.Context, storeID string) (map[string]string, error)
	SetStepOverride(ctx context.Context, storeID, stepID, status string) error
}
