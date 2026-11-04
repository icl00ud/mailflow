package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) SeedTenant(ctx context.Context, id, name, storeID string) (bool, error) {
	// Single statement: insert only when the table is empty, so a restart (or
	// two replicas booting together) cannot create a second tenant.
	tag, err := p.pool.Exec(ctx, `
INSERT INTO tenants (id, name)
SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM tenants)
ON CONFLICT (id) DO NOTHING`, id, name)
	if err != nil {
		return false, err
	}
	created := tag.RowsAffected() == 1
	if !created {
		return false, nil
	}
	if err := p.SetTenantStore(ctx, id, storeID); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Postgres) UpsertTenant(ctx context.Context, id, name string) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO tenants (id, name) VALUES ($1,$2)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`, id, name)
	return err
}

func (p *Postgres) TenantByID(ctx context.Context, id string) (*Tenant, error) {
	var t Tenant
	err := p.pool.QueryRow(ctx, `SELECT id, name, created_at FROM tenants WHERE id=$1`, id).
		Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (p *Postgres) TenantStores(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT store_id FROM tenant_stores WHERE tenant_id=$1 ORDER BY store_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (p *Postgres) SetTenantStore(ctx context.Context, tenantID, storeID string) error {
	// A store belongs to one tenant: re-mapping moves it instead of violating UNIQUE(store_id).
	_, err := p.pool.Exec(ctx, `
INSERT INTO tenant_stores (tenant_id, store_id) VALUES ($1,$2)
ON CONFLICT (store_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id`, tenantID, storeID)
	return err
}

func (p *Postgres) TenantOfStore(ctx context.Context, storeID string) (string, error) {
	var id string
	err := p.pool.QueryRow(ctx, `SELECT tenant_id FROM tenant_stores WHERE store_id=$1`, storeID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func (p *Postgres) CreateUser(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u := &User{ID: NewID(), Email: email}
	err := p.pool.QueryRow(ctx, `INSERT INTO users (id, email) VALUES ($1,$2)
ON CONFLICT (email) DO NOTHING RETURNING created_at`, u.ID, email).Scan(&u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExists
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (p *Postgres) UserByID(ctx context.Context, id string) (*User, error) {
	return p.scanUser(p.pool.QueryRow(ctx, `SELECT id, email, password_hash, created_at FROM users WHERE id=$1`, id))
}

func (p *Postgres) UserByEmail(ctx context.Context, email string) (*User, error) {
	return p.scanUser(p.pool.QueryRow(ctx, `SELECT id, email, password_hash, created_at FROM users WHERE email=$1`,
		strings.ToLower(strings.TrimSpace(email))))
}

func (p *Postgres) scanUser(r pgx.Row) (*User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (p *Postgres) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := p.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, hash)
	return err
}

func (p *Postgres) AddMembership(ctx context.Context, userID, tenantID, role string) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO memberships (user_id, tenant_id, role) VALUES ($1,$2,$3)
ON CONFLICT (user_id, tenant_id) DO UPDATE SET role = EXCLUDED.role`, userID, tenantID, role)
	return err
}

func (p *Postgres) Memberships(ctx context.Context, userID string) ([]Membership, error) {
	rows, err := p.pool.Query(ctx, `SELECT user_id, tenant_id, role FROM memberships WHERE user_id=$1 ORDER BY tenant_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.UserID, &m.TenantID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) RoleOf(ctx context.Context, userID, tenantID string) (string, error) {
	var role string
	err := p.pool.QueryRow(ctx, `SELECT role FROM memberships WHERE user_id=$1 AND tenant_id=$2`, userID, tenantID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

func (p *Postgres) StoreIDsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
SELECT DISTINCT ts.store_id
FROM memberships m JOIN tenant_stores ts ON ts.tenant_id = m.tenant_id
WHERE m.user_id=$1 ORDER BY ts.store_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateSession(ctx context.Context, s *Session) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES ($1,$2,$3,$4)`,
		s.TokenHash, s.UserID, s.ExpiresAt, s.CreatedAt)
	return err
}

func (p *Postgres) Session(ctx context.Context, tokenHash string, now time.Time) (*Session, error) {
	var s Session
	err := p.pool.QueryRow(ctx, `SELECT token_hash, user_id, expires_at, created_at FROM sessions
WHERE token_hash=$1 AND expires_at > $2`, tokenHash, now).
		Scan(&s.TokenHash, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (p *Postgres) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	return err
}

func (p *Postgres) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return err
}

func (p *Postgres) CreateInvite(ctx context.Context, inv *Invite) error {
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now()
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO invites (id, tenant_id, email, role, token_hash, expires_at, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		inv.ID, inv.TenantID, strings.ToLower(strings.TrimSpace(inv.Email)), inv.Role, inv.TokenHash, inv.ExpiresAt, inv.CreatedAt)
	return err
}

func (p *Postgres) InviteByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*Invite, error) {
	var inv Invite
	err := p.pool.QueryRow(ctx, `SELECT id, tenant_id, email, role, token_hash, expires_at, used_at, created_at
FROM invites WHERE token_hash=$1 AND used_at IS NULL AND expires_at > $2`, tokenHash, now).
		Scan(&inv.ID, &inv.TenantID, &inv.Email, &inv.Role, &inv.TokenHash, &inv.ExpiresAt, &inv.UsedAt, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &inv, err
}

func (p *Postgres) ConsumeInvite(ctx context.Context, id string, now time.Time) (bool, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE invites SET used_at=$2 WHERE id=$1 AND used_at IS NULL AND expires_at > $2`, id, now)
	return tag.RowsAffected() == 1, err
}

func (p *Postgres) CreatePasskey(ctx context.Context, k *Passkey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	tag, err := p.pool.Exec(ctx, `INSERT INTO webauthn_credentials
(id, user_id, public_key, sign_count, transports, name, created_at, last_used_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (id) DO NOTHING`,
		k.ID, k.UserID, k.PublicKey, k.SignCount, k.Transports, k.Name, k.CreatedAt, k.LastUsedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrExists
	}
	return nil
}

const passkeyCols = `id, user_id, public_key, sign_count, transports, name, created_at, last_used_at`

func scanPasskey(r pgx.Row) (*Passkey, error) {
	var k Passkey
	err := r.Scan(&k.ID, &k.UserID, &k.PublicKey, &k.SignCount, &k.Transports, &k.Name, &k.CreatedAt, &k.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &k, err
}

func (p *Postgres) PasskeysForUser(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+passkeyCols+` FROM webauthn_credentials WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		k, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (p *Postgres) PasskeyByID(ctx context.Context, id []byte) (*Passkey, error) {
	return scanPasskey(p.pool.QueryRow(ctx, `SELECT `+passkeyCols+` FROM webauthn_credentials WHERE id=$1`, id))
}

func (p *Postgres) TouchPasskey(ctx context.Context, id []byte, signCount int64, at time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE webauthn_credentials SET sign_count=$2, last_used_at=$3 WHERE id=$1`, id, signCount, at)
	return err
}

func (p *Postgres) DeletePasskey(ctx context.Context, id []byte, userID string) (bool, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id=$1 AND user_id=$2`, id, userID)
	return tag.RowsAffected() == 1, err
}

func (p *Postgres) PutChallenge(ctx context.Context, c *Challenge) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO webauthn_challenges (id, user_id, kind, data, expires_at)
VALUES ($1,$2,$3,$4,$5)`, c.ID, c.UserID, c.Kind, c.Data, c.ExpiresAt)
	return err
}

func (p *Postgres) TakeChallenge(ctx context.Context, id string, now time.Time) ([]byte, error) {
	var data []byte
	// DELETE ... RETURNING makes the challenge single use even with concurrent requests.
	err := p.pool.QueryRow(ctx, `DELETE FROM webauthn_challenges WHERE id=$1 AND expires_at > $2 RETURNING data`, id, now).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}
