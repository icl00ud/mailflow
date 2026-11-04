package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// ErrExists is returned when a unique key (email, credential id) is taken.
var ErrExists = errors.New("already exists")

// NewID returns a random 128-bit identifier as hex (user and invite ids).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// Roles a membership can carry. Owners may add tenants' stores and invite
// people; members can use the panel of the tenants they belong to.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"` // always lower-cased and trimmed
	PasswordHash string    `json:"-"`     // empty until an invite is accepted
	CreatedAt    time.Time `json:"created_at"`
}

type Membership struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
}

// Session is a server-side session; only the SHA-256 of the cookie token is stored.
type Session struct {
	TokenHash string    `json:"-"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Invite is a one-time link that lets somebody set a password for a tenant.
type Invite struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Passkey is one WebAuthn credential. ID is the raw credential id (bytea in
// Postgres), Transports is the JSON array the browser reported.
type Passkey struct {
	ID         []byte     `json:"id"`
	UserID     string     `json:"user_id"`
	PublicKey  []byte     `json:"-"`
	SignCount  int64      `json:"sign_count"`
	Transports string     `json:"transports"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Challenge is short-lived WebAuthn ceremony state (the library's SessionData
// as JSON). UserID is empty for usernameless login. Rows are single use.
type Challenge struct {
	ID        string
	UserID    string
	Kind      string // "register" | "login"
	Data      []byte
	ExpiresAt time.Time
}

// AuthStore is the tenancy/identity half of Store. It is embedded in Store so
// a caller only ever deals with one interface.
type AuthStore interface {
	// --- tenants ---
	// SeedTenant creates tenant id/name and maps storeID to it, but only when
	// no tenant exists at all. It reports whether it created anything.
	SeedTenant(ctx context.Context, id, name, storeID string) (bool, error)
	UpsertTenant(ctx context.Context, id, name string) error
	TenantByID(ctx context.Context, id string) (*Tenant, error)
	TenantStores(ctx context.Context, tenantID string) ([]string, error)
	// SetTenantStore maps a store to exactly one tenant; re-mapping moves it.
	SetTenantStore(ctx context.Context, tenantID, storeID string) error
	// TenantOfStore returns the tenant id owning storeID, or ErrNotFound.
	TenantOfStore(ctx context.Context, storeID string) (string, error)

	// --- users and memberships ---
	// CreateUser inserts a lower-cased email with no password; ErrExists if taken.
	CreateUser(ctx context.Context, email string) (*User, error)
	UserByID(ctx context.Context, id string) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
	// AddMembership inserts or updates the role of (user, tenant).
	AddMembership(ctx context.Context, userID, tenantID, role string) error
	Memberships(ctx context.Context, userID string) ([]Membership, error)
	// RoleOf returns the role, or ErrNotFound when there is no membership.
	RoleOf(ctx context.Context, userID, tenantID string) (string, error)
	// StoreIDsForUser is the set of stores the user may see, sorted.
	StoreIDsForUser(ctx context.Context, userID string) ([]string, error)

	// --- sessions ---
	CreateSession(ctx context.Context, s *Session) error
	// Session returns the live session for tokenHash; expired rows are ErrNotFound.
	Session(ctx context.Context, tokenHash string, now time.Time) (*Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error

	// --- invites ---
	CreateInvite(ctx context.Context, inv *Invite) error
	// InviteByTokenHash returns a single-use invite that is still valid.
	InviteByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*Invite, error)
	// ConsumeInvite marks the invite used; false when it was already used/expired.
	ConsumeInvite(ctx context.Context, id string, now time.Time) (bool, error)

	// --- passkeys ---
	CreatePasskey(ctx context.Context, p *Passkey) error
	PasskeysForUser(ctx context.Context, userID string) ([]Passkey, error)
	PasskeyByID(ctx context.Context, id []byte) (*Passkey, error)
	TouchPasskey(ctx context.Context, id []byte, signCount int64, at time.Time) error
	// DeletePasskey removes a credential owned by userID; false when not owned.
	DeletePasskey(ctx context.Context, id []byte, userID string) (bool, error)

	// --- webauthn challenges ---
	PutChallenge(ctx context.Context, c *Challenge) error
	// TakeChallenge deletes and returns the challenge data; ErrNotFound when
	// missing or expired, so a challenge can never be replayed.
	TakeChallenge(ctx context.Context, id string, now time.Time) ([]byte, error)
}
