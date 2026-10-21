package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Postgres struct{ pool *pgxpool.Pool }

// NewPostgres connects and applies the idempotent schema (CREATE ... IF NOT EXISTS).
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close()                         { p.pool.Close() }
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

const orderCols = `id, store_id, external_id, name, email, customer_name, currency, total, country, items,
 paid_at, tracking_number, carrier, tracking_url, dispatched_at, canceled, created_at`

func scanOrder(r pgx.Row) (*Order, error) {
	var o Order
	var items []byte
	err := r.Scan(&o.ID, &o.StoreID, &o.ExternalID, &o.Name, &o.Email, &o.CustomerName, &o.Currency, &o.Total,
		&o.Country, &items, &o.PaidAt, &o.Tracking, &o.Carrier, &o.TrackingURL, &o.DispatchedAt, &o.Canceled, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(items, &o.Items)
	return &o, nil
}

func (p *Postgres) UpsertOrder(ctx context.Context, o *Order) (bool, error) {
	items, _ := json.Marshal(o.Items)
	// xmax = 0 identifies a fresh insert; DO UPDATE no-op returns the existing id.
	var created bool
	err := p.pool.QueryRow(ctx, `
INSERT INTO orders (store_id, external_id, name, email, customer_name, currency, total, country, items, paid_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (store_id, external_id) DO UPDATE SET store_id = EXCLUDED.store_id
RETURNING id, (xmax = 0)`,
		o.StoreID, o.ExternalID, o.Name, o.Email, o.CustomerName, o.Currency, o.Total, o.Country, items, o.PaidAt).
		Scan(&o.ID, &created)
	return created, err
}

func (p *Postgres) OrderByID(ctx context.Context, id int64) (*Order, error) {
	return scanOrder(p.pool.QueryRow(ctx, `SELECT `+orderCols+` FROM orders WHERE id=$1`, id))
}

func (p *Postgres) OrderByExternal(ctx context.Context, storeID, ext string) (*Order, error) {
	return scanOrder(p.pool.QueryRow(ctx, `SELECT `+orderCols+` FROM orders WHERE store_id=$1 AND external_id=$2`, storeID, ext))
}

func (p *Postgres) ListOrders(ctx context.Context, storeID string, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `SELECT `+orderCols+` FROM orders WHERE ($1='' OR store_id=$1) ORDER BY paid_at DESC LIMIT $2`, storeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (p *Postgres) CancelOrder(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE orders SET canceled=true WHERE id=$1`, id)
	return err
}

func (p *Postgres) SetTracking(ctx context.Context, id int64, number, carrier, url string, at time.Time) (bool, error) {
	var changed bool
	err := p.pool.QueryRow(ctx, `
UPDATE orders o SET tracking_number=$2, carrier=$3, tracking_url=$4, dispatched_at=COALESCE(o.dispatched_at,$5)
FROM (SELECT id, tracking_number AS old FROM orders WHERE id=$1 FOR UPDATE) prev
WHERE o.id = prev.id
RETURNING prev.old IS DISTINCT FROM $2`, id, number, carrier, url, at).Scan(&changed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return changed, err
}

func (p *Postgres) AddEvent(ctx context.Context, e *Event) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
INSERT INTO tracking_events (order_id, event, label, source, at) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (order_id, event) DO NOTHING`, e.OrderID, e.Event, e.Label, e.Source, e.At)
	return tag.RowsAffected() == 1, err
}

func (p *Postgres) Events(ctx context.Context, orderID int64) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, order_id, event, label, source, at FROM tracking_events WHERE order_id=$1 ORDER BY at, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Event, &e.Label, &e.Source, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) Schedule(ctx context.Context, j *Job) (bool, error) {
	var created bool
	err := p.pool.QueryRow(ctx, `
INSERT INTO jobs (order_id, store_id, step_id, due_at) VALUES ($1,$2,$3,$4)
ON CONFLICT (order_id, step_id) DO UPDATE SET step_id = EXCLUDED.step_id
RETURNING id, (xmax = 0)`, j.OrderID, j.StoreID, j.StepID, j.DueAt).Scan(&j.ID, &created)
	return created, err
}

const jobCols = `id, order_id, store_id, step_id, due_at, status, held, attempts, reason, message_id, sent_at`

func scanJob(r pgx.Row) (*Job, error) {
	var j Job
	err := r.Scan(&j.ID, &j.OrderID, &j.StoreID, &j.StepID, &j.DueAt, &j.Status, &j.Held, &j.Attempts, &j.Reason, &j.MessageID, &j.SentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &j, err
}

func (p *Postgres) ClaimDue(ctx context.Context, now time.Time, limit int) ([]Job, error) {
	rows, err := p.pool.Query(ctx, `
UPDATE jobs SET status='sending', attempts=attempts+1, claimed_at=$1
WHERE id IN (
  SELECT id FROM jobs WHERE status='pending' AND due_at <= $1
  ORDER BY due_at LIMIT $2 FOR UPDATE SKIP LOCKED)
RETURNING `+jobCols, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (p *Postgres) Finish(ctx context.Context, id int64, status, reason, msgID string, sentAt *time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET status=$2, reason=$3, message_id=$4, sent_at=$5, held=false WHERE id=$1`,
		id, status, reason, msgID, sentAt)
	return err
}

func (p *Postgres) Defer(ctx context.Context, id int64, until time.Time, held bool, reason string) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET status='pending', due_at=$2, held=$3, reason=$4 WHERE id=$1`, id, until, held, reason)
	return err
}

func (p *Postgres) WakeHeld(ctx context.Context, orderID int64, now time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET due_at=$2 WHERE order_id=$1 AND status='pending' AND held`, orderID, now)
	return err
}

func (p *Postgres) Retry(ctx context.Context, id int64, now time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET status='pending', due_at=$2, reason='' WHERE id=$1 AND status IN ('failed','skipped')`, id, now)
	return err
}

func (p *Postgres) CancelPending(ctx context.Context, orderID int64, reason string) error {
	_, err := p.pool.Exec(ctx, `UPDATE jobs SET status='canceled', reason=$2 WHERE order_id=$1 AND status='pending'`, orderID, reason)
	return err
}

func (p *Postgres) FailStuck(ctx context.Context, before time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE jobs SET status='failed', reason='interrupted while sending; verify the inbox before retrying'
WHERE status='sending' AND claimed_at < $1`, before)
	return int(tag.RowsAffected()), err
}

func (p *Postgres) JobByID(ctx context.Context, id int64) (*Job, error) {
	return scanJob(p.pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=$1`, id))
}

func (p *Postgres) OrderJobs(ctx context.Context, orderID int64) ([]Job, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+jobCols+` FROM jobs WHERE order_id=$1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (p *Postgres) StepCounts(ctx context.Context, storeID string) (StepCounts, error) {
	rows, err := p.pool.Query(ctx, `SELECT step_id, status, count(*) FROM jobs WHERE store_id=$1 GROUP BY 1,2`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := StepCounts{}
	for rows.Next() {
		var step, status string
		var n int
		if err := rows.Scan(&step, &status, &n); err != nil {
			return nil, err
		}
		if out[step] == nil {
			out[step] = map[string]int{}
		}
		out[step][status] = n
	}
	return out, rows.Err()
}

func (p *Postgres) StepOverrides(ctx context.Context, storeID string) (map[string]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT step_id, status FROM step_overrides WHERE store_id=$1`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (p *Postgres) SetStepOverride(ctx context.Context, storeID, stepID, status string) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO step_overrides (store_id, step_id, status) VALUES ($1,$2,$3)
ON CONFLICT (store_id, step_id) DO UPDATE SET status=EXCLUDED.status`, storeID, stepID, status)
	return err
}
