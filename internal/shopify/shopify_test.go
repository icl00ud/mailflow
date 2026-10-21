package shopify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerify(t *testing.T) {
	body := []byte(`{"id":1}`)
	m := hmac.New(sha256.New, []byte("s3cret"))
	m.Write(body)
	good := base64.StdEncoding.EncodeToString(m.Sum(nil))
	if !Verify(body, good, "s3cret") {
		t.Fatal("valid signature rejected")
	}
	if Verify(body, good, "other") || Verify([]byte(`{"id":2}`), good, "s3cret") || Verify(body, "", "s3cret") || Verify(body, good, "") {
		t.Fatal("invalid signature accepted")
	}
}

func TestOrderAndFulfillment(t *testing.T) {
	o, err := Order("arman-curren", []byte(`{"id":820982911946154508,"name":"#1001","email":"a@b.co","currency":"USD",
	 "total_price":"194.98","processed_at":"2026-10-21T11:00:00-04:00","customer":{"first_name":"Alex","last_name":"Morgan"},
	 "shipping_address":{"country_code":"US"},"line_items":[{"title":"Watch","variant_title":"Blue","quantity":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.ExternalID != "820982911946154508" || o.Total != "$194.98" || o.CustomerName != "Alex Morgan" ||
		o.Country != "US" || len(o.Items) != 1 || o.Items[0].Qty != 2 || o.PaidAt.UTC().Hour() != 15 {
		t.Fatalf("bad mapping: %+v", o)
	}
	f, err := Parse([]byte(`{"order_id":5,"tracking_number":"LP1","tracking_company":"YunExpress","shipment_status":"out_for_delivery"}`))
	if err != nil || f.Number != "LP1" || f.Event != "out_for_delivery" {
		t.Fatalf("bad fulfillment: %+v %v", f, err)
	}
}
