package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is a Store for tests and local dev (DATABASE_URL unset). Not durable.
type Memory struct {
	mu        sync.Mutex
	seq       int64
	orders    map[int64]*Order
	jobs      map[int64]*Job
	events    []Event
	overrides map[string]map[string]string
}

func NewMemory() *Memory {
	return &Memory{orders: map[int64]*Order{}, jobs: map[int64]*Job{}, overrides: map[string]map[string]string{}}
}

func (m *Memory) next() int64 { m.seq++; return m.seq }

func (m *Memory) Ping(context.Context) error { return nil }

func (m *Memory) UpsertOrder(_ context.Context, o *Order) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.orders {
		if e.StoreID == o.StoreID && e.ExternalID == o.ExternalID {
			o.ID = e.ID
			return false, nil
		}
	}
	o.ID = m.next()
	o.CreatedAt = time.Now()
	c := *o
	m.orders[o.ID] = &c
	return true, nil
}

func (m *Memory) OrderByID(_ context.Context, id int64) (*Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *o
	return &c, nil
}

func (m *Memory) OrderByExternal(_ context.Context, storeID, ext string) (*Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.orders {
		if o.StoreID == storeID && o.ExternalID == ext {
			c := *o
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) ListOrders(_ context.Context, storeID string, limit int) ([]Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Order
	for _, o := range m.orders {
		if storeID == "" || o.StoreID == storeID {
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PaidAt.After(out[j].PaidAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) CancelOrder(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o, ok := m.orders[id]; ok {
		o.Canceled = true
	}
	return nil
}

func (m *Memory) SetTracking(_ context.Context, id int64, number, carrier, url string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return false, ErrNotFound
	}
	changed := o.Tracking != number
	o.Tracking, o.Carrier, o.TrackingURL = number, carrier, url
	if o.DispatchedAt == nil {
		t := at
		o.DispatchedAt = &t
	}
	return changed, nil
}

func (m *Memory) AddEvent(_ context.Context, e *Event) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.events {
		if x.OrderID == e.OrderID && x.Event == e.Event {
			return false, nil
		}
	}
	e.ID = m.next()
	m.events = append(m.events, *e)
	return true, nil
}

func (m *Memory) Events(_ context.Context, orderID int64) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, e := range m.events {
		if e.OrderID == orderID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *Memory) Schedule(_ context.Context, j *Job) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.jobs {
		if e.OrderID == j.OrderID && e.StepID == j.StepID {
			j.ID = e.ID
			return false, nil
		}
	}
	j.ID = m.next()
	if j.Status == "" {
		j.Status = JobPending
	}
	c := *j
	m.jobs[j.ID] = &c
	return true, nil
}

func (m *Memory) ClaimDue(_ context.Context, now time.Time, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []*Job
	for _, j := range m.jobs {
		if j.Status == JobPending && !j.DueAt.After(now) {
			due = append(due, j)
		}
	}
	sort.Slice(due, func(a, b int) bool { return due[a].DueAt.Before(due[b].DueAt) })
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	out := make([]Job, 0, len(due))
	for _, j := range due {
		j.Status = JobSending
		j.Attempts++
		out = append(out, *j)
	}
	return out, nil
}

func (m *Memory) Finish(_ context.Context, id int64, status, reason, msgID string, sentAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.Status, j.Reason, j.MessageID, j.SentAt, j.Held = status, reason, msgID, sentAt, false
	return nil
}

func (m *Memory) Defer(_ context.Context, id int64, until time.Time, held bool, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	j.Status, j.DueAt, j.Held, j.Reason = JobPending, until, held, reason
	return nil
}

func (m *Memory) WakeHeld(_ context.Context, orderID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.OrderID == orderID && j.Status == JobPending && j.Held {
			j.DueAt = now
		}
	}
	return nil
}

func (m *Memory) Retry(_ context.Context, id int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if j.Status == JobFailed || j.Status == JobSkipped {
		j.Status, j.DueAt, j.Reason = JobPending, now, ""
	}
	return nil
}

func (m *Memory) CancelPending(_ context.Context, orderID int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.OrderID == orderID && j.Status == JobPending {
			j.Status, j.Reason = JobCanceled, reason
		}
	}
	return nil
}

func (m *Memory) FailStuck(_ context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		if j.Status == JobSending && j.DueAt.Before(before) {
			j.Status, j.Reason = JobFailed, "interrupted while sending; verify the inbox before retrying"
			n++
		}
	}
	return n, nil
}

func (m *Memory) JobByID(_ context.Context, id int64) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *j
	return &c, nil
}

func (m *Memory) OrderJobs(_ context.Context, orderID int64) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Job
	for _, j := range m.jobs {
		if j.OrderID == orderID {
			out = append(out, *j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

func (m *Memory) StepCounts(_ context.Context, storeID string) (StepCounts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := StepCounts{}
	for _, j := range m.jobs {
		if j.StoreID != storeID {
			continue
		}
		if out[j.StepID] == nil {
			out[j.StepID] = map[string]int{}
		}
		out[j.StepID][j.Status]++
	}
	return out, nil
}

func (m *Memory) StepOverrides(_ context.Context, storeID string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.overrides[storeID] {
		out[k] = v
	}
	return out, nil
}

func (m *Memory) SetStepOverride(_ context.Context, storeID, stepID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.overrides[storeID] == nil {
		m.overrides[storeID] = map[string]string{}
	}
	m.overrides[storeID][stepID] = status
	return nil
}
