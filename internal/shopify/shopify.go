// Package shopify verifies and maps Shopify webhook payloads.
package shopify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/store"
)

// Verify checks X-Shopify-Hmac-Sha256 against the raw request body.
func Verify(body []byte, header, secret string) bool {
	if secret == "" || header == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(header))
}

type money = string

type orderPayload struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Email        string  `json:"email"`
	ContactEmail string  `json:"contact_email"`
	Currency     string  `json:"currency"`
	TotalPrice   money   `json:"total_price"`
	ProcessedAt  string  `json:"processed_at"`
	CreatedAt    string  `json:"created_at"`
	CancelledAt  *string `json:"cancelled_at"`
	Customer     struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"customer"`
	ShippingAddress struct {
		Name        string `json:"name"`
		CountryCode string `json:"country_code"`
	} `json:"shipping_address"`
	LineItems []struct {
		Title        string `json:"title"`
		VariantTitle string `json:"variant_title"`
		Quantity     int    `json:"quantity"`
	} `json:"line_items"`
}

// Order maps an orders/paid (or orders/create) payload. Amounts keep Shopify's string form.
func Order(storeID string, body []byte) (*store.Order, error) {
	var p orderPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		return nil, fmt.Errorf("order payload without id")
	}
	email := p.Email
	if email == "" {
		email = p.ContactEmail
	}
	name := strings.TrimSpace(p.Customer.FirstName + " " + p.Customer.LastName)
	if name == "" {
		name = p.ShippingAddress.Name
	}
	paid := time.Now().UTC()
	for _, s := range []string{p.ProcessedAt, p.CreatedAt} {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			paid = t.UTC()
			break
		}
	}
	o := &store.Order{
		StoreID: storeID, ExternalID: strconv.FormatInt(p.ID, 10), Name: p.Name, Email: email,
		CustomerName: name, Currency: p.Currency, Country: p.ShippingAddress.CountryCode, PaidAt: paid,
		Total: formatTotal(p.Currency, p.TotalPrice),
	}
	for _, li := range p.LineItems {
		o.Items = append(o.Items, store.Item{Title: li.Title, Variant: li.VariantTitle, Qty: li.Quantity})
	}
	return o, nil
}

func formatTotal(cur, amount string) string {
	if cur == "USD" {
		return "$" + amount
	}
	return strings.TrimSpace(amount + " " + cur)
}

// OrderID returns the Shopify order id of an orders/cancelled payload.
func OrderID(body []byte) (string, error) {
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.ID == 0 {
		return "", fmt.Errorf("payload without id")
	}
	return strconv.FormatInt(p.ID, 10), nil
}

// Fulfillment is a fulfillments/create|update payload reduced to what the flow needs.
type Fulfillment struct {
	OrderExternalID string
	Number, Carrier string
	URL             string
	ShipmentStatus  string
	Event           string // normalised flow.Ev*, "" when the status has no flow meaning
}

func Parse(body []byte) (*Fulfillment, error) {
	var p struct {
		OrderID        int64    `json:"order_id"`
		TrackingNumber string   `json:"tracking_number"`
		TrackingNums   []string `json:"tracking_numbers"`
		TrackingCo     string   `json:"tracking_company"`
		TrackingURL    string   `json:"tracking_url"`
		TrackingURLs   []string `json:"tracking_urls"`
		ShipmentStatus string   `json:"shipment_status"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.OrderID == 0 {
		return nil, fmt.Errorf("fulfillment payload without order_id")
	}
	f := &Fulfillment{OrderExternalID: strconv.FormatInt(p.OrderID, 10), Number: p.TrackingNumber,
		Carrier: p.TrackingCo, URL: p.TrackingURL, ShipmentStatus: p.ShipmentStatus}
	if f.Number == "" && len(p.TrackingNums) > 0 {
		f.Number = p.TrackingNums[0]
	}
	if f.URL == "" && len(p.TrackingURLs) > 0 {
		f.URL = p.TrackingURLs[0]
	}
	f.Event = EventFor(p.ShipmentStatus)
	return f, nil
}

// EventFor maps Shopify's shipment_status. Shopify has no "arrived in destination
// country" milestone; that one needs another source (tracking provider / panel).
func EventFor(status string) string {
	switch status {
	case "in_transit", "confirmed":
		return flow.EvInTransit
	case "out_for_delivery":
		return flow.EvOutForDelivery
	case "ready_for_pickup":
		return flow.EvReadyForPickup
	case "delivered":
		return flow.EvDelivered
	case "failure", "attempted_delivery":
		return flow.EvException
	}
	return ""
}
