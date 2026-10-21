package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Memory) {
	f, err := flow.Load("../../stores/arman-curren")
	if err != nil {
		t.Fatal(err)
	}
	f.Store.ShopifyDomain = "arman.myshopify.com"
	st := store.NewMemory()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, map[string]*flow.Flow{f.Store.ID: f}, mail.LogSender{Log: log}, log)
	s := &Server{Eng: eng, Log: log, AdminPassword: "pw", SenderName: "test",
		ShopifySecret: func(string) string { return "whsec" }}
	return httptest.NewServer(s.Handler()), st
}

func sign(body string) string {
	m := hmac.New(sha256.New, []byte("whsec"))
	m.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func post(t *testing.T, url, body string, hdr map[string]string) int {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestWebhookFlow(t *testing.T) {
	ts, st := newServer(t)
	defer ts.Close()
	body := `{"id":777,"name":"#1777","email":"a@b.co","currency":"USD","total_price":"89.99","processed_at":"2026-10-21T11:00:00-04:00",
	"customer":{"first_name":"Alex"},"line_items":[{"title":"Watch","quantity":1}]}`
	h := func(sig string) map[string]string {
		return map[string]string{"X-Shopify-Topic": "orders/paid", "X-Shopify-Shop-Domain": "arman.myshopify.com", "X-Shopify-Hmac-Sha256": sig}
	}
	if c := post(t, ts.URL+"/webhooks/shopify", body, h("bad")); c != 401 {
		t.Fatalf("bad signature -> %d", c)
	}
	if c := post(t, ts.URL+"/webhooks/shopify", body, h(sign(body))); c != 200 {
		t.Fatalf("valid -> %d", c)
	}
	if c := post(t, ts.URL+"/webhooks/shopify", body, h(sign(body))); c != 200 { // redelivery
		t.Fatalf("redelivery -> %d", c)
	}
	o, err := st.OrderByExternal(context.Background(), "arman-curren", "777")
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.OrderJobs(context.Background(), o.ID)
	if len(jobs) != 2 {
		t.Fatalf("jobs=%d want 2 (steps 1 and 2) after redelivery", len(jobs))
	}
	unknown := map[string]string{"X-Shopify-Shop-Domain": "evil.myshopify.com", "X-Shopify-Hmac-Sha256": sign(body)}
	if c := post(t, ts.URL+"/webhooks/shopify", body, unknown); c != 404 {
		t.Fatalf("unknown shop -> %d", c)
	}
}

func TestAdminAuth(t *testing.T) {
	ts, _ := newServer(t)
	defer ts.Close()
	r, _ := http.Get(ts.URL + "/api/flows")
	if r.StatusCode != 401 {
		t.Fatalf("no auth -> %d", r.StatusCode)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/flows", nil)
	req.SetBasicAuth("admin", "pw")
	r, _ = http.DefaultClient.Do(req)
	if r.StatusCode != 200 {
		t.Fatalf("auth -> %d", r.StatusCode)
	}
	req, _ = http.NewRequest("POST", ts.URL+"/api/orders/1/cancel", strings.NewReader("{}"))
	req.SetBasicAuth("admin", "pw")
	req.Header.Set("Content-Type", "text/plain")
	r, _ = http.DefaultClient.Do(req)
	if r.StatusCode != 415 {
		t.Fatalf("non-JSON POST -> %d want 415", r.StatusCode)
	}
}
