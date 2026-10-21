// Package track17 registers parcels with 17TRACK and turns its webhook pushes
// into the flow's normalised carrier events (Shopify has no "arrived in the
// destination country" milestone; 17TRACK does).
package track17

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/icl00ud/mailflow/internal/flow"
)

const defaultBase = "https://api.17track.net/track/v2.4"

type Client struct {
	Key  string
	Base string // override in tests
	HTTP *http.Client
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return defaultBase
}

// Tag links a 17TRACK push back to our order without a lookup table (max 100 chars).
func Tag(orderID int64) string { return "mf:" + strconv.FormatInt(orderID, 10) }

// OrderFromTag reverses Tag.
func OrderFromTag(tag string) (int64, bool) {
	rest, ok := strings.CutPrefix(tag, "mf:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

type registerResp struct {
	Code int `json:"code"`
	Data struct {
		Accepted []struct{ Number string } `json:"accepted"`
		Rejected []struct {
			Number string `json:"number"`
			Error  struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"rejected"`
	} `json:"data"`
}

// Register starts tracking a number. Registering an already-tracked number is not an error.
func (c *Client) Register(ctx context.Context, number, tag string) error {
	body, _ := json.Marshal([]map[string]any{{"number": number, "tag": tag, "lang": "en"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("17token", c.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("17track register: HTTP %d: %.200s", resp.StatusCode, raw)
	}
	var r registerResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("17track register: bad json: %w", err)
	}
	if r.Code != 0 {
		return fmt.Errorf("17track register: code %d: %.200s", r.Code, raw)
	}
	for _, rej := range r.Data.Rejected {
		if strings.Contains(strings.ToLower(rej.Error.Message), "registered") {
			return nil // already tracked: fine
		}
		return fmt.Errorf("17track rejected %s: %d %s", rej.Number, rej.Error.Code, rej.Error.Message)
	}
	return nil
}

// Verify checks the `sign` header: sha256_hex(rawBody + "/" + key).
func Verify(body []byte, sign, key string) bool {
	if key == "" || sign == "" {
		return false
	}
	sum := sha256.Sum256([]byte(string(body) + "/" + key))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(strings.TrimSpace(sign)))) == 1
}

type Event struct {
	Name  string // flow.Ev*
	Label string
	At    time.Time
}

type Push struct {
	Kind   string // TRACKING_UPDATED | TRACKING_STOPPED | ...
	Number string
	Tag    string
	Events []Event // chronological
}

type payload struct {
	Event string `json:"event"`
	Data  struct {
		Number    string `json:"number"`
		Tag       string `json:"tag"`
		TrackInfo struct {
			LatestStatus struct {
				Status    string `json:"status"`
				SubStatus string `json:"sub_status"`
			} `json:"latest_status"`
			Milestone []struct {
				KeyStage string `json:"key_stage"`
				TimeISO  string `json:"time_iso"`
				TimeUTC  string `json:"time_utc"`
			} `json:"milestone"`
		} `json:"track_info"`
	} `json:"data"`
}

var stageEvent = map[string]string{
	"PickedUp":           flow.EvInTransit,
	"Departure":          flow.EvInTransit,
	"Arrival":            flow.EvArrivedCountry, // "arrived at the destination country/region's port"
	"AvailableForPickup": flow.EvReadyForPickup,
	"OutForDelivery":     flow.EvOutForDelivery,
	"Delivered":          flow.EvDelivered,
}

var rank = map[string]int{flow.EvInTransit: 1, flow.EvArrivedCountry: 2, flow.EvReadyForPickup: 3, flow.EvOutForDelivery: 3, flow.EvDelivered: 4}

// Parse extracts the carrier milestones reached so far. When a later stage is
// already present (e.g. out for delivery on the first scan we ever see), the
// earlier "arrived" milestone is dropped: the customer should get one email
// for where the parcel is now, not two at once.
func Parse(body []byte) (*Push, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	out := &Push{Kind: p.Event, Number: p.Data.Number, Tag: p.Data.Tag}
	seen := map[string]Event{}
	for _, m := range p.Data.TrackInfo.Milestone {
		name, ok := stageEvent[m.KeyStage]
		if !ok {
			continue
		}
		ts := m.TimeUTC
		if ts == "" {
			ts = m.TimeISO
		}
		if ts == "" { // time absent = stage not reached
			continue
		}
		at, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		if prev, dup := seen[name]; !dup || at.Before(prev.At) {
			seen[name] = Event{Name: name, Label: m.KeyStage, At: at}
		}
	}
	// status is authoritative when milestones lag
	st := p.Data.TrackInfo.LatestStatus
	switch st.Status {
	case "Delivered":
		if _, ok := seen[flow.EvDelivered]; !ok {
			seen[flow.EvDelivered] = Event{Name: flow.EvDelivered, Label: st.SubStatus, At: time.Now().UTC()}
		}
	case "OutForDelivery":
		if _, ok := seen[flow.EvOutForDelivery]; !ok {
			seen[flow.EvOutForDelivery] = Event{Name: flow.EvOutForDelivery, Label: st.SubStatus, At: time.Now().UTC()}
		}
	case "AvailableForPickup":
		if _, ok := seen[flow.EvReadyForPickup]; !ok {
			seen[flow.EvReadyForPickup] = Event{Name: flow.EvReadyForPickup, Label: st.SubStatus, At: time.Now().UTC()}
		}
	case "Exception", "DeliveryFailure":
		seen[flow.EvException] = Event{Name: flow.EvException, Label: st.SubStatus, At: time.Now().UTC()}
	case "InTransit":
		if st.SubStatus == "InTransit_Arrival" {
			if _, ok := seen[flow.EvArrivedCountry]; !ok {
				seen[flow.EvArrivedCountry] = Event{Name: flow.EvArrivedCountry, Label: st.SubStatus, At: time.Now().UTC()}
			}
		}
	}
	maxRank := 0
	for n := range seen {
		if rank[n] > maxRank {
			maxRank = rank[n]
		}
	}
	for n, e := range seen {
		if n == flow.EvArrivedCountry && maxRank > rank[flow.EvArrivedCountry] {
			continue
		}
		out.Events = append(out.Events, e)
	}
	sort.Slice(out.Events, func(i, j int) bool {
		if !out.Events[i].At.Equal(out.Events[j].At) {
			return out.Events[i].At.Before(out.Events[j].At)
		}
		return rank[out.Events[i].Name] < rank[out.Events[j].Name]
	})
	return out, nil
}
