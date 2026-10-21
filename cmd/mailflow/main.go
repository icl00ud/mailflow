// mailflow sends the post-purchase email journey for our Shopify stores.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // stores run in America/New_York; do not depend on the image having zoneinfo

	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/server"
	"github.com/icl00ud/mailflow/internal/store"
	"github.com/icl00ud/mailflow/internal/track17"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
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

	var sender mail.Sender = mail.LogSender{Log: log}
	if host := os.Getenv("SMTP_HOST"); host != "" {
		sender = mail.SMTP{Host: host, Port: env("SMTP_PORT", "587"), User: os.Getenv("SMTP_USER"), Pass: os.Getenv("SMTP_PASS")}
	}
	log.Info("email sender", "kind", sender.Name())

	pass := os.Getenv("ADMIN_PASSWORD")
	if pass == "" {
		b := make([]byte, 9)
		_, _ = rand.Read(b)
		pass = hex.EncodeToString(b)
		log.Warn("ADMIN_PASSWORD unset: generated one for this run", "user", "admin", "password", pass)
	}

	eng := engine.New(st, flows, sender, log)
	srv := &server.Server{Eng: eng, Log: log, AdminPassword: pass, ShopifySecret: server.SecretFromEnv, SenderName: sender.Name()}

	if k := os.Getenv("TRACK17_KEY"); k != "" {
		eng.Tracker = &track17.Client{Key: k}
		srv.Track17Key = k
		log.Info("tracking provider", "kind", "17TRACK")
	} else {
		log.Warn("TRACK17_KEY unset: no automatic carrier events (steps 6/7 only via the panel)")
	}

	go eng.Run(ctx, 15*time.Second)

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
