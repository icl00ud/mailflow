package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/icl00ud/mailflow/internal/auth"
	"github.com/icl00ud/mailflow/internal/engine"
	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/mail"
	"github.com/icl00ud/mailflow/internal/store"
)

const (
	testPassword = "correct horse battery staple"
	tenantA      = "acme"
	tenantB      = "beta"
	storeA       = "arman-curren"
	storeB       = "store-b"
)

type fixture struct {
	ts  *httptest.Server
	st  *store.Memory
	eng *engine.Engine
	srv *Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fa, err := flow.Load("../../stores/arman-curren")
	if err != nil {
		t.Fatal(err)
	}
	fa.Store.ShopifyDomain = "arman.myshopify.com"
	fb, err := flow.Load("testdata/storeb")
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	ctx := context.Background()
	if _, err := st.SeedTenant(ctx, tenantA, "Acme", storeA); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTenant(ctx, tenantB, "Beta"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTenantStore(ctx, tenantB, storeB); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(st, map[string]*flow.Flow{fa.Store.ID: fa, fb.Store.ID: fb}, mail.LogSender{Log: log}, log)
	srv := &Server{Eng: eng, Log: log, SenderName: "test", CookieSecure: false,
		ShopifySecret: func(string) string { return "whsec" },
		Limiter:       auth.NewLimiter(10, 15*time.Minute)}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	srv.PublicOrigin = ts.URL
	// RP ID must be the request host: 127.0.0.1 for httptest.
	u, _ := url.Parse(ts.URL)
	wa, err := webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "mailflow", RPOrigins: []string{ts.URL}})
	if err != nil {
		t.Fatal(err)
	}
	srv.WebAuthn = wa
	return &fixture{ts: ts, st: st, eng: eng, srv: srv}
}

// user creates an account with a password and a membership of tenantID.
func (f *fixture) user(t *testing.T, email, tenantID, role string) *store.User {
	t.Helper()
	u, err := f.st.UserByEmail(context.Background(), email)
	if err == store.ErrNotFound {
		u, err = f.st.CreateUser(context.Background(), email)
	}
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetPasswordHash(context.Background(), u.ID, hash); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddMembership(context.Background(), u.ID, tenantID, role); err != nil {
		t.Fatal(err)
	}
	return u
}

// login returns the mf_session cookie value, failing the test on any non-303.
func (f *fixture) login(t *testing.T, email string) string {
	t.Helper()
	resp := f.form(t, "POST", "/login", url.Values{"email": {email}, "password": {testPassword}}, "")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s -> %d, want 303", email, resp.StatusCode)
	}
	return sessionCookieValue(t, resp)
}

func sessionCookieValue(t *testing.T, resp *http.Response) string {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatal("no session cookie in response")
	return ""
}

// req performs a request with the fixture origin, a session cookie and JSON body.
func (f *fixture) req(t *testing.T, method, path, body, cookie string) *http.Response {
	t.Helper()
	return f.reqH(t, method, path, body, cookie, true)
}

func (f *fixture) reqH(t *testing.T, method, path, body, cookie string, withOrigin bool) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if withOrigin {
		req.Header.Set("Origin", f.ts.URL)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// noRedirectClient keeps 303s visible: the tests assert on Location and on the
// Set-Cookie of the redirect itself.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (f *fixture) form(t *testing.T, method, path string, form url.Values, cookie string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.ts.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", f.ts.URL)
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- existing webhook coverage ----

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
	f := newFixture(t)
	body := `{"id":777,"name":"#1777","email":"a@b.co","currency":"USD","total_price":"89.99","processed_at":"2026-10-21T11:00:00-04:00",
	"customer":{"first_name":"Alex"},"line_items":[{"title":"Watch","quantity":1}]}`
	h := func(sig string) map[string]string {
		return map[string]string{"X-Shopify-Topic": "orders/paid", "X-Shopify-Shop-Domain": "arman.myshopify.com", "X-Shopify-Hmac-Sha256": sig}
	}
	// Webhooks carry their own HMAC auth and no Origin: the CSRF gate must not
	// touch them.
	if c := post(t, f.ts.URL+"/webhooks/shopify", body, h("bad")); c != 401 {
		t.Fatalf("bad signature -> %d", c)
	}
	if c := post(t, f.ts.URL+"/webhooks/shopify", body, h(sign(body))); c != 200 {
		t.Fatalf("valid -> %d", c)
	}
	if c := post(t, f.ts.URL+"/webhooks/shopify", body, h(sign(body))); c != 200 { // redelivery
		t.Fatalf("redelivery -> %d", c)
	}
	o, err := f.st.OrderByExternal(context.Background(), storeA, "777")
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := f.st.OrderJobs(context.Background(), o.ID)
	if len(jobs) != 2 {
		t.Fatalf("jobs=%d want 2 (steps 1 and 2) after redelivery", len(jobs))
	}
	unknown := map[string]string{"X-Shopify-Shop-Domain": "evil.myshopify.com", "X-Shopify-Hmac-Sha256": sign(body)}
	if c := post(t, f.ts.URL+"/webhooks/shopify", body, unknown); c != 404 {
		t.Fatalf("unknown shop -> %d", c)
	}
}

type fakeTracker struct{ got []string }

func (f *fakeTracker) Register(_ context.Context, number, tag string) error {
	f.got = append(f.got, number+"|"+tag)
	return nil
}

func TestTrack17WebhookDrivesStep6(t *testing.T) {
	f := newFixture(t)
	f.srv.Track17Key = "K17"
	ft := &fakeTracker{}
	f.eng.Tracker = ft

	o := &store.Order{StoreID: storeA, ExternalID: "1", Email: "a@b.co", Name: "#1", PaidAt: f.eng.Now()}
	if _, err := f.eng.OrderPaid(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.Dispatched(context.Background(), o.ID, "LP9", "Yun", "", f.eng.Now()); err != nil {
		t.Fatal(err)
	}
	if len(ft.got) != 1 || ft.got[0] != "LP9|mf:"+strconv.FormatInt(o.ID, 10) {
		t.Fatalf("registration: %v", ft.got)
	}
	if err := f.eng.Dispatched(context.Background(), o.ID, "LP9", "Yun", "", f.eng.Now()); err != nil || len(ft.got) != 1 {
		t.Fatalf("same number must not re-register: %v %v", ft.got, err)
	}

	payload := `{"event":"TRACKING_UPDATED","data":{"number":"LP9","carrier":3011,"tag":"mf:` + strconv.FormatInt(o.ID, 10) + `","track_info":{"latest_status":{"status":"InTransit","sub_status":"InTransit_Arrival"},"milestone":[{"key_stage":"Arrival","time_iso":"2026-10-26T01:00:00Z","time_utc":"2026-10-26T01:00:00Z"}]}}}`
	sum := sha256.Sum256([]byte(payload + "/K17"))
	if c := post(t, f.ts.URL+"/webhooks/17track", payload, map[string]string{"sign": "deadbeef"}); c != 401 {
		t.Fatalf("bad sign -> %d", c)
	}
	for i := 0; i < 2; i++ { // redelivery is a no-op
		if c := post(t, f.ts.URL+"/webhooks/17track", payload, map[string]string{"sign": hex.EncodeToString(sum[:])}); c != 200 {
			t.Fatalf("signed push -> %d", c)
		}
	}
	jobs, _ := f.st.OrderJobs(context.Background(), o.ID)
	n6 := 0
	for _, j := range jobs {
		if j.StepID == "6" {
			n6++
		}
	}
	if n6 != 1 {
		t.Fatalf("step 6 jobs=%d want exactly 1", n6)
	}
}

// ---- authentication ----

func TestPanelRequiresSession(t *testing.T) {
	f := newFixture(t)
	if resp := f.req(t, "GET", "/", "", ""); resp.StatusCode != 303 || resp.Header.Get("Location") != "/login" {
		t.Fatalf("panel without session -> %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := f.req(t, "GET", "/account", "", ""); resp.StatusCode != 303 ||
		resp.Header.Get("Location") != "/login?next=%2Faccount" {
		t.Fatalf("account without session -> %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := f.req(t, "GET", "/api/flows", "", ""); resp.StatusCode != 401 {
		t.Fatalf("api without session -> %d", resp.StatusCode)
	}
	if resp := f.reqH(t, "GET", "/healthz", "", "", false); resp.StatusCode != 200 {
		t.Fatalf("healthz must stay public -> %d", resp.StatusCode)
	}
	// An unknown cookie is as good as no cookie.
	if resp := f.req(t, "GET", "/api/flows", "", "not-a-real-token"); resp.StatusCode != 401 {
		t.Fatalf("forged cookie -> %d", resp.StatusCode)
	}
}

func TestLoginGenericError(t *testing.T) {
	f := newFixture(t)
	f.user(t, "a@example.com", tenantA, store.RoleOwner)
	wrong := f.form(t, "POST", "/login", url.Values{"email": {"a@example.com"}, "password": {"nope"}}, "")
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password -> %d", wrong.StatusCode)
	}
	unknown := f.form(t, "POST", "/login", url.Values{"email": {"ghost@example.com"}, "password": {testPassword}}, "")
	if unknown.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown email -> %d", unknown.StatusCode)
	}
	if wb, ub := body(t, wrong), body(t, unknown); !strings.Contains(wb, msgBadLogin) || !strings.Contains(ub, msgBadLogin) {
		t.Fatalf("both failures must show the same generic message")
	}
	// A user created by invite has no password yet and cannot log in with "".
	noPw, err := f.st.CreateUser(context.Background(), "new@example.com")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.st.AddMembership(context.Background(), noPw.ID, tenantA, store.RoleMember)
	if resp := f.form(t, "POST", "/login", url.Values{"email": {"new@example.com"}, "password": {""}}, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("passwordless user -> %d", resp.StatusCode)
	}
}

func TestSessionLifecycle(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "owner@example.com", tenantA, store.RoleOwner)

	resp := f.form(t, "POST", "/login", url.Values{"email": {"OWNER@example.com "}, "password": {testPassword}}, "")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login -> %d", resp.StatusCode)
	}
	var c *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			c = ck
		}
	}
	if c == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !c.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite=%v want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Fatalf("session cookie path=%q", c.Path)
	}
	if c.Secure {
		t.Fatal("cookie must not be Secure in this test (COOKIE_SECURE=false)")
	}
	if c.MaxAge < 29*24*3600 || c.MaxAge > 31*24*3600 {
		t.Fatalf("cookie MaxAge=%d want ~30 days", c.MaxAge)
	}
	// The plaintext token is never stored.
	if _, err := f.st.Session(context.Background(), c.Value, time.Now()); err == nil {
		t.Fatal("session is keyed by the hash, not by the cookie value")
	}
	if _, err := f.st.Session(context.Background(), auth.HashToken(c.Value), time.Now()); err != nil {
		t.Fatalf("hashed session lookup: %v", err)
	}

	if resp := f.req(t, "GET", "/api/flows", "", c.Value); resp.StatusCode != 200 {
		t.Fatalf("authenticated flows -> %d", resp.StatusCode)
	}
	if resp := f.req(t, "GET", "/api/me", "", c.Value); resp.StatusCode != 200 ||
		!strings.Contains(body(t, resp), u.Email) {
		t.Fatalf("api/me did not identify the user")
	}

	if resp := f.req(t, "POST", "/logout", "", c.Value); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout -> %d", resp.StatusCode)
	}
	if _, err := f.st.Session(context.Background(), auth.HashToken(c.Value), time.Now()); err != store.ErrNotFound {
		t.Fatal("logout must delete the server-side session row")
	}
	if resp := f.req(t, "GET", "/api/flows", "", c.Value); resp.StatusCode != 401 {
		t.Fatalf("reused cookie after logout -> %d", resp.StatusCode)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	f := newFixture(t)
	u := f.user(t, "old@example.com", tenantA, store.RoleMember)
	token := "expired-token"
	now := time.Now()
	if err := f.st.CreateSession(context.Background(), &store.Session{
		TokenHash: auth.HashToken(token), UserID: u.ID, ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-31 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if resp := f.req(t, "GET", "/api/flows", "", token); resp.StatusCode != 401 {
		t.Fatalf("expired session -> %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	f.user(t, "target@example.com", tenantA, store.RoleMember)
	for i := 0; i < 10; i++ {
		if resp := f.form(t, "POST", "/login", url.Values{"email": {"target@example.com"}, "password": {"wrong"}}, ""); resp.StatusCode != 401 {
			t.Fatalf("attempt %d -> %d", i+1, resp.StatusCode)
		}
	}
	// The 11th attempt is refused before the password is even checked.
	if resp := f.form(t, "POST", "/login", url.Values{"email": {"target@example.com"}, "password": {testPassword}}, ""); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("11th attempt -> %d want 429", resp.StatusCode)
	}
	// A different account is not affected.
	if resp := f.form(t, "POST", "/login", url.Values{"email": {"other@example.com"}, "password": {"wrong"}}, ""); resp.StatusCode != 401 {
		t.Fatalf("other account -> %d", resp.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	f := newFixture(t)
	f.user(t, "owner@example.com", tenantA, store.RoleOwner)
	cookie := f.login(t, "owner@example.com")

	if resp := f.reqH(t, "POST", "/api/flows/"+storeA+"/steps/1/status", `{"status":"off"}`, cookie, false); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without Origin/Referer -> %d want 403", resp.StatusCode)
	}
	// A mismatching Origin is rejected even if a same-origin Referer is also sent.
	req, _ := http.NewRequest("POST", f.ts.URL+"/api/flows/"+storeA+"/steps/1/status", strings.NewReader(`{"status":"off"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Referer", f.ts.URL+"/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", sessionCookie+"="+cookie)
	resp, err := f.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST with evil Origin -> %d want 403", resp.StatusCode)
	}
	if _, err := f.st.Session(context.Background(), auth.HashToken(cookie), time.Now()); err != nil {
		t.Fatal("csrf test lost its session")
	}
	// Referer alone is accepted (older clients).
	req, _ = http.NewRequest("POST", f.ts.URL+"/api/flows/"+storeA+"/steps/1/status", strings.NewReader(`{"status":"off"}`))
	req.Header.Set("Referer", f.ts.URL+"/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", sessionCookie+"="+cookie)
	resp, err = f.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST with same-origin Referer -> %d want 200", resp.StatusCode)
	}
	// And the normal Origin path works.
	if resp := f.req(t, "POST", "/api/flows/"+storeA+"/steps/1/status", `{"status":"on"}`, cookie); resp.StatusCode != 200 {
		t.Fatalf("POST with Origin -> %d", resp.StatusCode)
	}
	// GETs need no Origin.
	if resp := f.reqH(t, "GET", "/api/flows", "", cookie, false); resp.StatusCode != 200 {
		t.Fatalf("GET without Origin -> %d", resp.StatusCode)
	}
}

func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	f.user(t, "a@example.com", tenantA, store.RoleOwner)
	f.user(t, "b@example.com", tenantB, store.RoleMember)
	ca, cb := f.login(t, "a@example.com"), f.login(t, "b@example.com")

	// Each user sees exactly their tenant's store.
	var flowsA, flowsB []flowView
	resp := f.req(t, "GET", "/api/flows", "", ca)
	if err := json.Unmarshal([]byte(body(t, resp)), &flowsA); err != nil {
		t.Fatal(err)
	}
	resp = f.req(t, "GET", "/api/flows", "", cb)
	if err := json.Unmarshal([]byte(body(t, resp)), &flowsB); err != nil {
		t.Fatal(err)
	}
	if len(flowsA) != 1 || flowsA[0].Store.ID != storeA {
		t.Fatalf("tenant A flows: %+v", flowsA)
	}
	if len(flowsB) != 1 || flowsB[0].Store.ID != storeB {
		t.Fatalf("tenant B flows: %+v", flowsB)
	}

	// Acting on another tenant's store is 404, never 403.
	if resp := f.req(t, "POST", "/api/flows/"+storeB+"/steps/1/status", `{"status":"off"}`, ca); resp.StatusCode != 404 {
		t.Fatalf("A -> B step status: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "GET", "/api/flows/"+storeB+"/steps/1/preview", "", ca); resp.StatusCode != 404 {
		t.Fatalf("A -> B preview: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "POST", "/api/flows/"+storeB+"/steps/1/test", `{"to":"x@y.co"}`, ca); resp.StatusCode != 404 {
		t.Fatalf("A -> B test send: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "POST", "/api/orders", `{"store":"store-b","email":"x@y.co"}`, ca); resp.StatusCode != 404 {
		t.Fatalf("A -> create order in B: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "GET", "/api/orders?store="+storeB, "", ca); resp.StatusCode != 404 {
		t.Fatalf("A -> list B orders: %d want 404", resp.StatusCode)
	}

	// A real order + job in B: invisible and untouchable for A.
	o := &store.Order{StoreID: storeB, ExternalID: "b1", Email: "c@d.co", Name: "#B1", PaidAt: f.eng.Now()}
	if _, err := f.eng.OrderPaid(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	jobs, _ := f.st.OrderJobs(context.Background(), o.ID)
	if len(jobs) == 0 {
		t.Fatal("no jobs scheduled for the B order")
	}
	if resp := f.req(t, "POST", "/api/orders/"+strconv.FormatInt(o.ID, 10)+"/cancel", "", ca); resp.StatusCode != 404 {
		t.Fatalf("A -> cancel B order: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "POST", "/api/orders/"+strconv.FormatInt(o.ID, 10)+"/tracking", `{"number":"X1"}`, ca); resp.StatusCode != 404 {
		t.Fatalf("A -> track B order: %d want 404", resp.StatusCode)
	}
	if resp := f.req(t, "POST", "/api/jobs/"+strconv.FormatInt(jobs[0].ID, 10)+"/retry", "", ca); resp.StatusCode != 404 {
		t.Fatalf("A -> retry B job: %d want 404", resp.StatusCode)
	}
	// The unfiltered order list hides B's order from A and shows it to B.
	var ordersA, ordersB []orderView
	resp = f.req(t, "GET", "/api/orders", "", ca)
	if err := json.Unmarshal([]byte(body(t, resp)), &ordersA); err != nil {
		t.Fatal(err)
	}
	resp = f.req(t, "GET", "/api/orders", "", cb)
	if err := json.Unmarshal([]byte(body(t, resp)), &ordersB); err != nil {
		t.Fatal(err)
	}
	if len(ordersA) != 0 {
		t.Fatalf("A sees %d orders from another tenant", len(ordersA))
	}
	if len(ordersB) != 1 || ordersB[0].StoreID != storeB {
		t.Fatalf("B orders: %+v", ordersB)
	}

	// B can act on its own store.
	if resp := f.req(t, "POST", "/api/flows/"+storeB+"/steps/1/status", `{"status":"off"}`, cb); resp.StatusCode != 200 {
		t.Fatalf("B -> own step status: %d", resp.StatusCode)
	}
	if resp := f.req(t, "POST", "/api/orders/"+strconv.FormatInt(o.ID, 10)+"/cancel", "", cb); resp.StatusCode != 200 {
		t.Fatalf("B -> cancel own order: %d", resp.StatusCode)
	}
	// And B cannot reach A's store.
	if resp := f.req(t, "POST", "/api/flows/"+storeA+"/steps/1/status", `{"status":"off"}`, cb); resp.StatusCode != 404 {
		t.Fatalf("B -> A store: %d want 404", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	resp := f.req(t, "GET", "/login", "", "")
	h := resp.Header
	if h.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options=%q", h.Get("X-Frame-Options"))
	}
	if h.Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("Referrer-Policy=%q", h.Get("Referrer-Policy"))
	}
	csp := h.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("CSP allows inline/eval scripts: %q", csp)
	}
	// The panel's JS is served from /static, and its markup has no inline script.
	if resp := f.req(t, "GET", "/static/passkey.js", "", ""); resp.StatusCode != 200 {
		t.Fatalf("static asset -> %d", resp.StatusCode)
	}
	page := body(t, f.req(t, "GET", "/login", "", ""))
	if strings.Contains(page, "<script>") {
		t.Fatal("login page must not use an inline <script>")
	}
}

func TestInviteSingleUseAndExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()

	newInvite := func(email string, exp time.Time) (string, *store.Invite) {
		tok, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		inv := &store.Invite{ID: store.NewID(), TenantID: tenantB, Email: email, Role: store.RoleOwner,
			TokenHash: auth.HashToken(tok), ExpiresAt: exp, CreatedAt: now}
		if err := f.st.CreateInvite(ctx, inv); err != nil {
			t.Fatal(err)
		}
		return tok, inv
	}

	token, inv := newInvite("newbie@example.com", now.Add(48*time.Hour))
	if resp := f.req(t, "GET", "/invite/"+token, "", ""); resp.StatusCode != 200 {
		t.Fatalf("invite page -> %d", resp.StatusCode)
	}
	short := f.form(t, "POST", "/invite/"+token, url.Values{"password": {"12345678901"}, "password_confirm": {"12345678901"}}, "")
	if short.StatusCode != 200 || !strings.Contains(body(t, short), "12 caracteres") {
		t.Fatalf("11-char password accepted")
	}
	mismatch := f.form(t, "POST", "/invite/"+token, url.Values{"password": {testPassword}, "password_confirm": {testPassword + "x"}}, "")
	if mismatch.StatusCode != 200 || !strings.Contains(body(t, mismatch), "não coincidem") {
		t.Fatalf("mismatched passwords accepted")
	}
	// Still unused after the rejected attempts.
	if _, err := f.st.InviteByTokenHash(ctx, auth.HashToken(token), now); err != nil {
		t.Fatalf("rejected attempts consumed the invite: %v", err)
	}

	ok := f.form(t, "POST", "/invite/"+token, url.Values{"password": {testPassword}, "password_confirm": {testPassword}}, "")
	if ok.StatusCode != http.StatusSeeOther || ok.Header.Get("Location") != "/account?welcome=1" {
		t.Fatalf("invite accept -> %d %q", ok.StatusCode, ok.Header.Get("Location"))
	}
	cookie := sessionCookieValue(t, ok)
	u, err := f.st.UserByEmail(ctx, inv.Email)
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == "" || !auth.VerifyPassword(u.PasswordHash, testPassword) {
		t.Fatal("password was not stored")
	}
	if role, err := f.st.RoleOf(ctx, u.ID, tenantB); err != nil || role != store.RoleOwner {
		t.Fatalf("membership: %q %v", role, err)
	}
	// The new account works with its password.
	if resp := f.req(t, "GET", "/api/flows", "", cookie); resp.StatusCode != 200 {
		t.Fatalf("session from invite -> %d", resp.StatusCode)
	}
	// Second use is a 410 and does not change the password.
	again := f.form(t, "POST", "/invite/"+token, url.Values{"password": {"another-long-password"}, "password_confirm": {"another-long-password"}}, "")
	if again.StatusCode != http.StatusGone {
		t.Fatalf("second use -> %d want 410", again.StatusCode)
	}
	if u2, _ := f.st.UserByEmail(ctx, inv.Email); !auth.VerifyPassword(u2.PasswordHash, testPassword) {
		t.Fatal("reused invite changed the password")
	}

	expiredToken, _ := newInvite("late@example.com", now.Add(-time.Minute))
	if resp := f.req(t, "GET", "/invite/"+expiredToken, "", ""); resp.StatusCode != http.StatusGone {
		t.Fatalf("expired invite page -> %d want 410", resp.StatusCode)
	}
	if resp := f.form(t, "POST", "/invite/"+expiredToken, url.Values{"password": {testPassword}, "password_confirm": {testPassword}}, ""); resp.StatusCode != http.StatusGone {
		t.Fatalf("expired invite accept -> %d want 410", resp.StatusCode)
	}
	if _, err := f.st.UserByEmail(ctx, "late@example.com"); err != store.ErrNotFound {
		t.Fatal("expired invite created an account")
	}
}

func TestPasskeyCeremonies(t *testing.T) {
	f := newFixture(t)
	f.user(t, "pk@example.com", tenantA, store.RoleOwner)
	cookie := f.login(t, "pk@example.com")

	resp := f.req(t, "POST", "/api/passkeys/register/begin", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("register begin -> %d", resp.StatusCode)
	}
	var begin struct {
		ChallengeID string `json:"challenge_id"`
		Options     struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
				RP        struct {
					ID string `json:"id"`
				} `json:"rp"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(body(t, resp)), &begin); err != nil {
		t.Fatal(err)
	}
	if begin.ChallengeID == "" || begin.Options.PublicKey.Challenge == "" {
		t.Fatalf("begin registration returned nothing usable: %+v", begin)
	}
	if begin.Options.PublicKey.RP.ID != "127.0.0.1" {
		t.Fatalf("rp id=%q want the PUBLIC_ORIGIN host", begin.Options.PublicKey.RP.ID)
	}
	// A garbage credential burns the challenge; the replay is refused.
	bad := f.req(t, "POST", "/api/passkeys/register/finish?cid="+url.QueryEscape(begin.ChallengeID), `{"id":"AAAA","type":"public-key","response":{}}`, cookie)
	if bad.StatusCode != 400 {
		t.Fatalf("garbage registration -> %d", bad.StatusCode)
	}
	replay := f.req(t, "POST", "/api/passkeys/register/finish?cid="+url.QueryEscape(begin.ChallengeID), `{"id":"AAAA","type":"public-key","response":{}}`, cookie)
	if replay.StatusCode != 400 {
		t.Fatalf("challenge replay -> %d want 400", replay.StatusCode)
	}
	if n, _ := f.st.PasskeysForUser(context.Background(), f.mustUser(t, "pk@example.com").ID); len(n) != 0 {
		t.Fatalf("a failed ceremony stored %d passkeys", len(n))
	}

	// Login begin issues a usernameless challenge; same single-use rule.
	resp = f.req(t, "POST", "/api/passkeys/login/begin", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("login begin -> %d", resp.StatusCode)
	}
	var lb struct {
		ChallengeID string `json:"challenge_id"`
		Options     struct {
			PublicKey struct {
				Challenge        string `json:"challenge"`
				UserVerification string `json:"userVerification"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(body(t, resp)), &lb); err != nil {
		t.Fatal(err)
	}
	if lb.ChallengeID == "" || lb.Options.PublicKey.Challenge == "" {
		t.Fatal("begin login returned nothing usable")
	}
	if lb.Options.PublicKey.UserVerification != "preferred" {
		t.Fatalf("userVerification=%q", lb.Options.PublicKey.UserVerification)
	}
	// The challenge is consumed on the first (failed) attempt.
	if r := f.req(t, "POST", "/api/passkeys/login/finish?cid="+url.QueryEscape(lb.ChallengeID), `{"id":"AAAA"}`, ""); r.StatusCode != 401 {
		t.Fatalf("garbage login -> %d want 401", r.StatusCode)
	}
	if r := f.req(t, "POST", "/api/passkeys/login/finish?cid="+url.QueryEscape(lb.ChallengeID), `{"id":"AAAA"}`, ""); r.StatusCode != 400 {
		t.Fatalf("login challenge replay -> %d", r.StatusCode)
	}
	// A registration challenge cannot be used at the login endpoint.
	resp = f.req(t, "POST", "/api/passkeys/register/begin", "", cookie)
	var rb struct {
		ChallengeID string `json:"challenge_id"`
	}
	_ = json.Unmarshal([]byte(body(t, resp)), &rb)
	if r := f.req(t, "POST", "/api/passkeys/login/finish?cid="+url.QueryEscape(rb.ChallengeID), `{"id":"AAAA"}`, ""); r.StatusCode != 400 {
		t.Fatalf("cross-kind challenge -> %d", r.StatusCode)
	}

	// Listing and deleting only ever touches the caller's own credentials.
	if resp := f.req(t, "GET", "/api/passkeys", "", cookie); resp.StatusCode != 200 || strings.TrimSpace(body(t, resp)) != "[]" {
		t.Fatalf("empty passkey list -> %d %q", resp.StatusCode, body(t, resp))
	}
	if resp := f.req(t, "POST", "/api/passkeys/AAAAAAAAAAAAAAAAAAAAAA/delete", "", cookie); resp.StatusCode != 404 {
		t.Fatalf("delete unknown passkey -> %d want 404", resp.StatusCode)
	}
}

func TestSafeNext(t *testing.T) {
	for _, in := range []string{"", "https://evil.example/", "//evil.example/x", "/\\evil.example", "/a\r\nSet-Cookie: x=1"} {
		if got := safeNext(in); got != "/" {
			t.Fatalf("safeNext(%q)=%q want /", in, got)
		}
	}
	if got := safeNext("/account?welcome=1"); got != "/account?welcome=1" {
		t.Fatalf("safeNext kept a legitimate path? %q", got)
	}
	// The login redirect ignores a hostile ?next.
	f := newFixture(t)
	f.user(t, "owner@example.com", tenantA, store.RoleOwner)
	resp := f.form(t, "POST", "/login", url.Values{"email": {"owner@example.com"}, "password": {testPassword},
		"next": {"//evil.example/"}}, "")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("hostile next -> %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func (f *fixture) mustUser(t *testing.T, email string) *store.User {
	t.Helper()
	u, err := f.st.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTenantSeededFromEmptyStore(t *testing.T) {
	st := store.NewMemory()
	created, err := st.SeedTenant(context.Background(), "arman-curren", "Arman Curren", "arman-curren")
	if err != nil || !created {
		t.Fatalf("seed: created=%v err=%v", created, err)
	}
	again, err := st.SeedTenant(context.Background(), "other", "Other", "other")
	if err != nil || again {
		t.Fatalf("second seed must be a no-op: created=%v err=%v", again, err)
	}
	tid, err := st.TenantOfStore(context.Background(), "arman-curren")
	if err != nil || tid != "arman-curren" {
		t.Fatalf("store owner: %q %v", tid, err)
	}
}
