package store

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// authContract is the tenancy/identity half of the store contract: identical
// expectations for Memory and Postgres.
func authContract(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	// --- tenants: seeding happens once ---
	created, err := s.SeedTenant(ctx, "arman-curren", "Arman Curren", "arman-curren")
	if err != nil || !created {
		t.Fatalf("seed tenant: created=%v err=%v", created, err)
	}
	if again, err := s.SeedTenant(ctx, "other", "Other", "other"); err != nil || again {
		t.Fatalf("seed must be a no-op when a tenant exists: created=%v err=%v", again, err)
	}
	if id, err := s.TenantOfStore(ctx, "arman-curren"); err != nil || id != "arman-curren" {
		t.Fatalf("tenant of store: %q %v", id, err)
	}
	if _, err := s.TenantOfStore(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("unknown store tenant err=%v", err)
	}

	// --- a second tenant with its own store; a store belongs to one tenant ---
	if err := s.UpsertTenant(ctx, "beta", "Beta"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertTenant(ctx, "beta", "Beta Renamed"); err != nil {
		t.Fatal(err)
	}
	if tnt, err := s.TenantByID(ctx, "beta"); err != nil || tnt.Name != "Beta Renamed" {
		t.Fatalf("upsert tenant: %+v %v", tnt, err)
	}
	if err := s.SetTenantStore(ctx, "beta", "store-b"); err != nil {
		t.Fatal(err)
	}
	if stores, err := s.TenantStores(ctx, "beta"); err != nil || len(stores) != 1 || stores[0] != "store-b" {
		t.Fatalf("tenant stores: %v %v", stores, err)
	}

	// --- users ---
	u, err := s.CreateUser(ctx, "  Owner@Example.COM ")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "owner@example.com" {
		t.Fatalf("email not normalised: %q", u.Email)
	}
	if u.ID == "" || u.PasswordHash != "" {
		t.Fatalf("new user: %+v", u)
	}
	if _, err := s.CreateUser(ctx, "owner@example.com"); err != ErrExists {
		t.Fatalf("duplicate email err=%v want ErrExists", err)
	}
	if got, err := s.UserByEmail(ctx, "OWNER@example.com"); err != nil || got.ID != u.ID {
		t.Fatalf("lookup by mixed-case email: %+v %v", got, err)
	}
	if _, err := s.UserByEmail(ctx, "missing@example.com"); err != ErrNotFound {
		t.Fatalf("missing user err=%v", err)
	}
	if err := s.SetPasswordHash(ctx, u.ID, "$argon2id$fake"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByID(ctx, u.ID); got.PasswordHash != "$argon2id$fake" {
		t.Fatalf("password hash not stored: %q", got.PasswordHash)
	}

	// --- memberships ---
	if err := s.AddMembership(ctx, u.ID, "arman-curren", RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMembership(ctx, u.ID, "arman-curren", RoleMember); err != nil { // update
		t.Fatal(err)
	}
	if role, err := s.RoleOf(ctx, u.ID, "arman-curren"); err != nil || role != RoleMember {
		t.Fatalf("role: %q %v", role, err)
	}
	if _, err := s.RoleOf(ctx, u.ID, "beta"); err != ErrNotFound {
		t.Fatalf("non-member role err=%v", err)
	}
	if err := s.AddMembership(ctx, u.ID, "beta", RoleOwner); err != nil {
		t.Fatal(err)
	}
	if mems, err := s.Memberships(ctx, u.ID); err != nil || len(mems) != 2 {
		t.Fatalf("memberships: %+v %v", mems, err)
	}
	if stores, err := s.StoreIDsForUser(ctx, u.ID); err != nil || len(stores) != 2 ||
		stores[0] != "arman-curren" || stores[1] != "store-b" {
		t.Fatalf("store ids: %v %v", stores, err)
	}
	lone, err := s.CreateUser(ctx, "lone@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if stores, err := s.StoreIDsForUser(ctx, lone.ID); err != nil || len(stores) != 0 {
		t.Fatalf("a user with no membership must see nothing: %v %v", stores, err)
	}

	// --- sessions ---
	sess := &Session{TokenHash: "hash-1", UserID: u.ID, ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(ctx, "hash-1", now)
	if err != nil || got.UserID != u.ID {
		t.Fatalf("session: %+v %v", got, err)
	}
	if _, err := s.Session(ctx, "hash-1", now.Add(31*24*time.Hour)); err != ErrNotFound {
		t.Fatalf("expired session err=%v want ErrNotFound", err)
	}
	if _, err := s.Session(ctx, "no-such-hash", now); err != ErrNotFound {
		t.Fatalf("missing session err=%v", err)
	}
	if err := s.DeleteSession(ctx, "hash-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(ctx, "hash-1", now); err != ErrNotFound {
		t.Fatalf("deleted session err=%v", err)
	}
	// DeleteExpiredSessions removes only stale rows.
	for _, tt := range []struct {
		hash string
		exp  time.Time
	}{{"live", now.Add(time.Hour)}, {"stale", now.Add(-time.Hour)}} {
		if err := s.CreateSession(ctx, &Session{TokenHash: tt.hash, UserID: u.ID, ExpiresAt: tt.exp, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteExpiredSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session(ctx, "stale", now.Add(-2*time.Hour)); err != ErrNotFound {
		t.Fatalf("stale session survived: %v", err)
	}
	if err := s.DeleteSession(ctx, "live"); err != nil {
		t.Fatal(err)
	}

	// --- invites: single use, 48h ---
	inv := &Invite{ID: "inv-1", TenantID: "beta", Email: "Invitee@Example.com", Role: RoleOwner,
		TokenHash: "tok-1", ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now}
	if err := s.CreateInvite(ctx, inv); err != nil {
		t.Fatal(err)
	}
	gotInv, err := s.InviteByTokenHash(ctx, "tok-1", now)
	if err != nil || gotInv.ID != "inv-1" || gotInv.Email != "invitee@example.com" || gotInv.Role != RoleOwner {
		t.Fatalf("invite: %+v %v", gotInv, err)
	}
	if _, err := s.InviteByTokenHash(ctx, "tok-1", now.Add(49*time.Hour)); err != ErrNotFound {
		t.Fatalf("expired invite err=%v", err)
	}
	if ok, err := s.ConsumeInvite(ctx, "inv-1", now); err != nil || !ok {
		t.Fatalf("first consume: %v %v", ok, err)
	}
	if ok, err := s.ConsumeInvite(ctx, "inv-1", now); err != nil || ok {
		t.Fatalf("second consume: %v %v", ok, err)
	}
	if _, err := s.InviteByTokenHash(ctx, "tok-1", now); err != ErrNotFound {
		t.Fatalf("used invite err=%v", err)
	}
	expired := &Invite{ID: "inv-2", TenantID: "beta", Email: "late@example.com", Role: RoleMember,
		TokenHash: "tok-2", ExpiresAt: now.Add(-time.Minute), CreatedAt: now}
	if err := s.CreateInvite(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ConsumeInvite(ctx, "inv-2", now); ok {
		t.Fatal("an expired invite was consumed")
	}

	// --- passkeys ---
	credID := []byte{0x01, 0x02, 0x03, 0x04}
	k := &Passkey{ID: credID, UserID: u.ID, PublicKey: []byte{0xaa, 0xbb}, SignCount: 0,
		Transports: `["internal"]`, Name: "laptop", CreatedAt: now}
	if err := s.CreatePasskey(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePasskey(ctx, &Passkey{ID: credID, UserID: u.ID, PublicKey: []byte{1}}); err != ErrExists {
		t.Fatalf("duplicate credential err=%v want ErrExists", err)
	}
	keys, err := s.PasskeysForUser(ctx, u.ID)
	if err != nil || len(keys) != 1 || keys[0].Name != "laptop" || !bytes.Equal(keys[0].PublicKey, []byte{0xaa, 0xbb}) {
		t.Fatalf("passkeys: %+v %v", keys, err)
	}
	byID, err := s.PasskeyByID(ctx, credID)
	if err != nil || byID.UserID != u.ID || byID.SignCount != 0 {
		t.Fatalf("passkey by id: %+v %v", byID, err)
	}
	if err := s.TouchPasskey(ctx, credID, 7, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if byID, _ := s.PasskeyByID(ctx, credID); byID.SignCount != 7 || byID.LastUsedAt == nil {
		t.Fatalf("touch passkey: %+v", byID)
	}
	if del, err := s.DeletePasskey(ctx, credID, lone.ID); err != nil || del {
		t.Fatalf("another user must not delete the passkey: %v %v", del, err)
	}
	if del, err := s.DeletePasskey(ctx, credID, u.ID); err != nil || !del {
		t.Fatalf("owner delete: %v %v", del, err)
	}
	if _, err := s.PasskeyByID(ctx, credID); err != ErrNotFound {
		t.Fatalf("deleted passkey err=%v", err)
	}

	// --- challenges: short lived and single use ---
	if err := s.PutChallenge(ctx, &Challenge{ID: "ch-1", UserID: u.ID, Kind: "register",
		Data: []byte(`{"kind":"register"}`), ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	data, err := s.TakeChallenge(ctx, "ch-1", now)
	if err != nil || len(data) == 0 {
		t.Fatalf("take challenge: %q %v", data, err)
	}
	if _, err := s.TakeChallenge(ctx, "ch-1", now); err != ErrNotFound {
		t.Fatalf("challenge replay err=%v want ErrNotFound", err)
	}
	if err := s.PutChallenge(ctx, &Challenge{ID: "ch-2", Kind: "login", Data: []byte("x"), ExpiresAt: now.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeChallenge(ctx, "ch-2", now); err != ErrNotFound {
		t.Fatalf("expired challenge err=%v", err)
	}
}
