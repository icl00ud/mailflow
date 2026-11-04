// mailflow sends the post-purchase email journey for our Shopify stores.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // stores run in America/New_York; do not depend on the image having zoneinfo

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/icl00ud/mailflow/internal/auth"
	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/server"
	"github.com/icl00ud/mailflow/internal/store"
	"github.com/icl00ud/mailflow/internal/track17"
)

const inviteTTL = 48 * time.Hour

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func boolEnv(k string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func publicOrigin() string {
	return strings.TrimRight(env("PUBLIC_ORIGIN", server.DefaultOrigin), "/")
}

func main() {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "invite":
			os.Exit(runInvite(os.Args[2:]))
		case "tenant":
			os.Exit(runTenant(os.Args[2:]))
		case "help":
			usage(os.Stdout)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
			usage(os.Stderr)
			os.Exit(2)
		}
	}
	serve()
}

func usage(w *os.File) {
	fmt.Fprint(w, `mailflow

  mailflow                     run the panel and the email engine
  mailflow -check              validate flows and templates, then exit
  mailflow invite --tenant ID --email EMAIL [--role owner|member]
  mailflow tenant add --id ID --name NAME --store STORE_ID

Environment: DATABASE_URL, PUBLIC_ORIGIN, COOKIE_SECURE, WEBAUTHN_RP_ID,
WEBAUTHN_RP_NAME, SHOPIFY_WEBHOOK_SECRET[_STORE], TRACK17_KEY, SMTP_*.
`)
}

func serve() {
	storesDir := flag.String("stores", env("STORES_DIR", "stores"), "directory with <store>/flow.yaml")
	addr := flag.String("addr", env("LISTEN_ADDR", ":8080"), "listen address")
	check := flag.Bool("check", false, "validate stores and templates, then exit")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	flows, err := flow.LoadAll(*storesDir)
	if err != nil {
		log.Error("load stores", "err", err)
		os.Exit(1)
	}
	rend := mail.NewRenderer()
	for _, f := range flows {
		if err := rend.Preload(f); err != nil {
			log.Error("templates", "err", err)
			os.Exit(1)
		}
	}
	if *check {
		log.Info("ok", "stores", len(flows))
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var st store.Store
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(ctx, dsn)
		if err != nil {
			log.Error("postgres", "err", err)
			os.Exit(1)
		}
		defer pg.Close()
		st = pg
	} else {
		log.Warn("DATABASE_URL unset: using the in-memory store (dev only, nothing persists)")
		st = store.NewMemory()
	}

	// First boot: create the original tenant so the existing store keeps working.
	// The in-memory store starts empty too, which keeps dev and prod identical.
	if created, err := st.SeedTenant(ctx, "arman-curren", "Arman Curren", "arman-curren"); err != nil {
		log.Error("seed tenant", "err", err)
		os.Exit(1)
	} else if created {
		log.Info("seeded tenant", "tenant", "arman-curren", "store", "arman-curren")
	}

	origin := publicOrigin()
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Error("PUBLIC_ORIGIN is not a full origin (https://host)", "value", origin)
		os.Exit(1)
	}
	rpID := env("WEBAUTHN_RP_ID", u.Hostname())
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: env("WEBAUTHN_RP_NAME", "mailflow"),
		RPOrigins:     []string{origin},
	})
	if err != nil {
		log.Error("webauthn config", "err", err, "rp_id", rpID, "origin", origin)
		os.Exit(1)
	}
	log.Info("panel auth", "origin", origin, "webauthn_rp_id", rpID, "cookie_secure", boolEnv("COOKIE_SECURE", true))

	var sender mail.Sender = mail.LogSender{Log: log}
	if host := os.Getenv("SMTP_HOST"); host != "" {
		sender = mail.SMTP{Host: host, Port: env("SMTP_PORT", "587"), User: os.Getenv("SMTP_USER"), Pass: os.Getenv("SMTP_PASS")}
	}
	log.Info("email sender", "kind", sender.Name())

	eng := engine.New(st, flows, sender, log)
	srv := &server.Server{Eng: eng, Log: log, SenderName: sender.Name(),
		ShopifySecret: server.SecretFromEnv,
		PublicOrigin:  origin,
		CookieSecure:  boolEnv("COOKIE_SECURE", true),
		WebAuthn:      wa,
		Limiter:       auth.NewLimiter(10, 15*time.Minute),
	}
	if u.Scheme != "https" {
		log.Warn("PUBLIC_ORIGIN is not https: the session cookie will not be Secure unless COOKIE_SECURE is set", "origin", origin)
	}

	if k := os.Getenv("TRACK17_KEY"); k != "" {
		eng.Tracker = &track17.Client{Key: k}
		srv.Track17Key = k
		log.Info("tracking provider", "kind", "17TRACK")
	} else {
		log.Warn("TRACK17_KEY unset: no automatic carrier events (steps 6/7 only via the panel)")
	}

	go eng.Run(ctx, 15*time.Second)

	// Housekeeping: expired sessions and stale WebAuthn challenges would
	// otherwise accumulate forever. Cheap enough to run every few hours.
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			if err := st.DeleteExpiredSessions(ctx, time.Now()); err != nil {
				log.Warn("purge sessions", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = hs.Shutdown(sc)
	}()
	log.Info("listening", "addr", *addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// openPostgres is the CLI's connection path: same DSN, same idempotent schema.
func openPostgres(ctx context.Context) (*store.Postgres, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required for this command")
	}
	return store.NewPostgres(ctx, dsn)
}

func runInvite(args []string) int {
	fs := flag.NewFlagSet("invite", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id the person joins")
	email := fs.String("email", "", "email address to invite")
	role := fs.String("role", store.RoleMember, "owner or member")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tenant == "" || *email == "" {
		fmt.Fprintln(os.Stderr, "usage: mailflow invite --tenant TENANT --email EMAIL [--role owner|member]")
		return 2
	}
	if *role != store.RoleOwner && *role != store.RoleMember {
		fmt.Fprintln(os.Stderr, "--role must be owner or member")
		return 2
	}
	if !strings.Contains(*email, "@") {
		fmt.Fprintln(os.Stderr, "--email must be an email address")
		return 2
	}

	ctx := context.Background()
	pg, err := openPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pg.Close()

	t, err := pg.TenantByID(ctx, *tenant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tenant %q does not exist (create it with: mailflow tenant add --id %s --name ... --store ...)\n", *tenant, *tenant)
		return 1
	}
	u, err := pg.UserByEmail(ctx, *email)
	if errors.Is(err, store.ErrNotFound) {
		u, err = pg.CreateUser(ctx, *email)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "user: %v\n", err)
		return 1
	}
	if err := pg.AddMembership(ctx, u.ID, t.ID, *role); err != nil {
		fmt.Fprintf(os.Stderr, "membership: %v\n", err)
		return 1
	}
	token, err := auth.NewToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "token: %v\n", err)
		return 1
	}
	now := time.Now()
	inv := &store.Invite{ID: store.NewID(), TenantID: t.ID, Email: u.Email, Role: *role,
		TokenHash: auth.HashToken(token), ExpiresAt: now.Add(inviteTTL), CreatedAt: now}
	if err := pg.CreateInvite(ctx, inv); err != nil {
		fmt.Fprintf(os.Stderr, "invite: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "invite for %s (tenant %s, role %s) expires %s\n",
		u.Email, t.ID, *role, inv.ExpiresAt.UTC().Format(time.RFC3339))
	fmt.Printf("%s/invite/%s\n", publicOrigin(), token)
	return 0
}

func runTenant(args []string) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(os.Stderr, "usage: mailflow tenant add --id ID --name NAME --store STORE")
		return 2
	}
	fs := flag.NewFlagSet("tenant add", flag.ContinueOnError)
	id := fs.String("id", "", "tenant id (slug)")
	name := fs.String("name", "", "display name")
	storeID := fs.String("store", "", "store id from stores/<id>/flow.yaml")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *id == "" || *name == "" || *storeID == "" {
		fmt.Fprintln(os.Stderr, "usage: mailflow tenant add --id ID --name NAME --store STORE")
		return 2
	}
	ctx := context.Background()
	pg, err := openPostgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pg.Close()
	if err := pg.UpsertTenant(ctx, *id, *name); err != nil {
		fmt.Fprintf(os.Stderr, "tenant: %v\n", err)
		return 1
	}
	if err := pg.SetTenantStore(ctx, *id, *storeID); err != nil {
		fmt.Fprintf(os.Stderr, "store mapping: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "tenant %s (%s) now owns store %s\n", *id, *name, *storeID)
	return 0
}
