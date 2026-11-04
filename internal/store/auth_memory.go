package store

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Memory keeps the same tenancy/identity contract as Postgres so the panel can
// run with DATABASE_URL unset (dev, tests). Everything disappears on restart.

func (m *Memory) SeedTenant(_ context.Context, id, name, storeID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tenantByID) > 0 {
		return false, nil
	}
	m.tenantByID[id] = &Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	m.storeTenant[storeID] = id
	return true, nil
}

func (m *Memory) UpsertTenant(_ context.Context, id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tenantByID[id]; ok {
		t.Name = name
		return nil
	}
	m.tenantByID[id] = &Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	return nil
}

func (m *Memory) TenantByID(_ context.Context, id string) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tenantByID[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *t
	return &c, nil
}

func (m *Memory) TenantStores(_ context.Context, tenantID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for storeID, tid := range m.storeTenant {
		if tid == tenantID {
			out = append(out, storeID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) SetTenantStore(_ context.Context, tenantID, storeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeTenant[storeID] = tenantID
	return nil
}

func (m *Memory) TenantOfStore(_ context.Context, storeID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.storeTenant[storeID]
	if !ok {
		return "", ErrNotFound
	}
	return id, nil
}

func (m *Memory) CreateUser(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, ok := m.userByEmail[email]; ok {
		return nil, ErrExists
	}
	u := &User{ID: NewID(), Email: email, CreatedAt: time.Now()}
	m.userByID[u.ID] = u
	m.userByEmail[email] = u.ID
	c := *u
	return &c, nil
}

func (m *Memory) UserByID(_ context.Context, id string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.userByID[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *u
	return &c, nil
}

func (m *Memory) UserByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.userByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil, ErrNotFound
	}
	c := *m.userByID[id]
	return &c, nil
}

func (m *Memory) SetPasswordHash(_ context.Context, userID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.userByID[userID]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = hash
	return nil
}

func (m *Memory) AddMembership(_ context.Context, userID, tenantID, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.memberships[userID] == nil {
		m.memberships[userID] = map[string]string{}
	}
	m.memberships[userID][tenantID] = role
	return nil
}

func (m *Memory) Memberships(_ context.Context, userID string) ([]Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Membership
	for tenantID, role := range m.memberships[userID] {
		out = append(out, Membership{UserID: userID, TenantID: tenantID, Role: role})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

func (m *Memory) RoleOf(_ context.Context, userID, tenantID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.memberships[userID][tenantID]
	if !ok {
		return "", ErrNotFound
	}
	return role, nil
}

func (m *Memory) StoreIDsForUser(_ context.Context, userID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for tenantID := range m.memberships[userID] {
		for storeID, tid := range m.storeTenant {
			if tid == tenantID {
				seen[storeID] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) CreateSession(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	c := *s
	m.sessions[s.TokenHash] = &c
	return nil
}

func (m *Memory) Session(_ context.Context, tokenHash string, now time.Time) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tokenHash]
	if !ok || !s.ExpiresAt.After(now) {
		return nil, ErrNotFound
	}
	c := *s
	return &c, nil
}

func (m *Memory) DeleteSession(_ context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

func (m *Memory) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, hash)
		}
	}
	return nil
}

func (m *Memory) CreateInvite(_ context.Context, inv *Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now()
	}
	c := *inv
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	m.invites[c.ID] = &c
	return nil
}

func (m *Memory) InviteByTokenHash(_ context.Context, tokenHash string, now time.Time) (*Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inv := range m.invites {
		if inv.TokenHash != tokenHash {
			continue
		}
		if inv.UsedAt != nil || !inv.ExpiresAt.After(now) {
			return nil, ErrNotFound
		}
		c := *inv
		return &c, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) ConsumeInvite(_ context.Context, id string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.invites[id]
	if !ok || inv.UsedAt != nil || !inv.ExpiresAt.After(now) {
		return false, nil
	}
	t := now
	inv.UsedAt = &t
	return true, nil
}

func (m *Memory) CreatePasskey(_ context.Context, k *Passkey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(k.ID)
	if _, ok := m.passkeys[key]; ok {
		return ErrExists
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	c := *k
	c.ID = append([]byte(nil), k.ID...)
	c.PublicKey = append([]byte(nil), k.PublicKey...)
	m.passkeys[key] = &c
	return nil
}

func (m *Memory) PasskeysForUser(_ context.Context, userID string) ([]Passkey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Passkey
	for _, k := range m.passkeys {
		if k.UserID == userID {
			c := *k
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i].ID) < string(out[j].ID) })
	return out, nil
}

func (m *Memory) PasskeyByID(_ context.Context, id []byte) (*Passkey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.passkeys[string(id)]
	if !ok {
		return nil, ErrNotFound
	}
	c := *k
	return &c, nil
}

func (m *Memory) TouchPasskey(_ context.Context, id []byte, signCount int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.passkeys[string(id)]
	if !ok {
		return ErrNotFound
	}
	k.SignCount = signCount
	k.LastUsedAt = &at
	return nil
}

func (m *Memory) DeletePasskey(_ context.Context, id []byte, userID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(id)
	k, ok := m.passkeys[key]
	if !ok || k.UserID != userID {
		return false, nil
	}
	delete(m.passkeys, key)
	return true, nil
}

func (m *Memory) PutChallenge(_ context.Context, c *Challenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	cp.Data = append([]byte(nil), c.Data...)
	m.challenges[c.ID] = &cp
	return nil
}

func (m *Memory) TakeChallenge(_ context.Context, id string, now time.Time) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.challenges[id]
	if !ok {
		return nil, ErrNotFound
	}
	delete(m.challenges, id)
	if !c.ExpiresAt.After(now) {
		return nil, ErrNotFound
	}
	return c.Data, nil
}
