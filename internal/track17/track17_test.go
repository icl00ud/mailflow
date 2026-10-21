package track17

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icl00ud/mailflow/internal/flow"
)

func TestVerify(t *testing.T) {
	body := []byte(`{"event":"TRACKING_UPDATED","data":{"number":"RR1","carrier":3011,"tag":null}}`)
	sum := sha256.Sum256([]byte(string(body) + "/123456ABCDEF"))
	good := hex.EncodeToString(sum[:])
	if !Verify(body, good, "123456ABCDEF") || !Verify(body, strings.ToUpper(good), "123456ABCDEF") {
		t.Fatal("valid signature rejected")
	}
	if Verify(body, good, "other") || Verify([]byte("x"), good, "123456ABCDEF") || Verify(body, "", "123456ABCDEF") || Verify(body, good, "") {
		t.Fatal("invalid signature accepted")
	}
}

func TestTagRoundTrip(t *testing.T) {
	if id, ok := OrderFromTag(Tag(42)); !ok || id != 42 {
		t.Fatal("tag round trip")
	}
	for _, bad := range []string{"", "42", "mf:", "mf:x", "mf:-1"} {
		if _, ok := OrderFromTag(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func ms(stage, utc string) string {
	if utc == "" {
		return `{"key_stage":"` + stage + `","time_iso":null,"time_utc":null}`
	}
	return `{"key_stage":"` + stage + `","time_iso":"` + utc + `","time_utc":"` + utc + `"}`
}

func push(status, sub string, stages ...string) []byte {
	return []byte(`{"event":"TRACKING_UPDATED","data":{"number":"LP1","carrier":3011,"tag":"mf:7","track_info":{
	"latest_status":{"status":"` + status + `","sub_status":"` + sub + `"},"milestone":[` + strings.Join(stages, ",") + `]}}}`)
}

func names(p *Push) []string {
	var n []string
	for _, e := range p.Events {
		n = append(n, e.Name)
	}
	return n
}

func TestParseArrival(t *testing.T) {
	p, err := Parse(push("InTransit", "InTransit_Arrival",
		ms("InfoReceived", ""), ms("PickedUp", "2026-10-20T01:00:00Z"), ms("Departure", "2026-10-21T01:00:00Z"), ms("Arrival", "2026-10-26T01:00:00Z"), ms("Delivered", "")))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(p), ","); got != "in_transit,arrived_country" || p.Tag != "mf:7" || p.Number != "LP1" {
		t.Fatalf("got %s %+v", got, p)
	}
}

func TestParseArrivalFromStatusOnly(t *testing.T) {
	p, _ := Parse(push("InTransit", "InTransit_Arrival"))
	if got := strings.Join(names(p), ","); got != "arrived_country" {
		t.Fatalf("got %s", got)
	}
}

func TestParseSkipsStaleArrivalWhenParcelAlreadyOutForDelivery(t *testing.T) {
	p, _ := Parse(push("OutForDelivery", "OutForDelivery_Other",
		ms("Arrival", "2026-10-26T01:00:00Z"), ms("OutForDelivery", "2026-10-28T12:00:00Z")))
	if got := strings.Join(names(p), ","); got != "out_for_delivery" {
		t.Fatalf("got %s: only one email for where the parcel is now", got)
	}
}

func TestParseDeliveredAndException(t *testing.T) {
	p, _ := Parse(push("Delivered", "Delivered_Other"))
	if got := strings.Join(names(p), ","); got != flow.EvDelivered {
		t.Fatalf("got %s", got)
	}
	p, _ = Parse(push("Exception", "Exception_Returned"))
	if got := strings.Join(names(p), ","); got != flow.EvException {
		t.Fatalf("got %s", got)
	}
	p, _ = Parse(push("NotFound", "NotFound_Other"))
	if len(p.Events) != 0 {
		t.Fatal("NotFound must produce no events")
	}
}

func TestRegister(t *testing.T) {
	var gotBody, gotToken string
	reply := `{"code":0,"data":{"accepted":[{"number":"LP1"}],"rejected":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotToken = string(b), r.Header.Get("17token")
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	c := &Client{Key: "K", Base: srv.URL}
	if err := c.Register(context.Background(), "LP1", Tag(7)); err != nil {
		t.Fatal(err)
	}
	if gotToken != "K" || !strings.Contains(gotBody, `"number":"LP1"`) || !strings.Contains(gotBody, `"tag":"mf:7"`) {
		t.Fatalf("request: token=%q body=%s", gotToken, gotBody)
	}
	reply = `{"code":0,"data":{"accepted":[],"rejected":[{"number":"LP1","error":{"code":-18019901,"message":"The tracking number has been registered."}}]}}`
	if err := c.Register(context.Background(), "LP1", Tag(7)); err != nil {
		t.Fatalf("already registered must be ok: %v", err)
	}
	reply = `{"code":0,"data":{"accepted":[],"rejected":[{"number":"LP1","error":{"code":-18019903,"message":"Carrier cannot be detected."}}]}}`
	if err := c.Register(context.Background(), "LP1", Tag(7)); err == nil {
		t.Fatal("real rejection must be an error")
	}
	reply = `{"code":-1}`
	if err := c.Register(context.Background(), "LP1", Tag(7)); err == nil {
		t.Fatal("non-zero code must be an error")
	}
}
