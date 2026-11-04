// Package flow defines a store's post-purchase email journey: ordered steps,
// what triggers each one, how long it waits, and when it must be skipped.
package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/icl00ud/mailflow/internal/bizhours"
	"gopkg.in/yaml.v3"
)

const (
	TriggerOrderPaid = "order_paid"
	TriggerAfterStep = "after_step"
	TriggerTracking  = "tracking"

	StatusOn    = "on"    // sends
	StatusOff   = "off"   // written but paused
	StatusDraft = "draft" // copy not written yet; never sends
)

// Normalised carrier events (see tracking mapping in internal/shopify and the API).
const (
	EvInTransit      = "in_transit"
	EvArrivedCountry = "arrived_country"
	EvOutForDelivery = "out_for_delivery"
	EvReadyForPickup = "ready_for_pickup"
	EvDelivered      = "delivered"
	EvException      = "exception"
)

var validEvents = map[string]bool{EvInTransit: true, EvArrivedCountry: true, EvOutForDelivery: true,
	EvReadyForPickup: true, EvDelivered: true, EvException: true}

// Conditions. "hold" conditions keep the job pending until true; the rest skip it for good.
const (
	CondHasTracking   = "has_tracking"        // hold: needs a real tracking number
	CondNoCarrierMove = "no_carrier_movement" // skip if the carrier already reported anything
	CondNotArrived    = "not_arrived"         // skip if parcel already reached destination country
	CondNotDelivered  = "not_delivered"       // skip once delivered
	CondWithinWindow  = "within_window"       // skip if now is past dispatch + window_days
)

var validConds = map[string]bool{CondHasTracking: true, CondNoCarrierMove: true, CondNotArrived: true,
	CondNotDelivered: true, CondWithinWindow: true}

type SendWindow struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

type StoreCfg struct {
	ID         string     `yaml:"id" json:"id"`
	Name       string     `yaml:"name" json:"name"`
	From       string     `yaml:"from" json:"from"`
	ReplyTo    string     `yaml:"reply_to" json:"reply_to"`
	Site       string     `yaml:"site" json:"site"`
	Timezone   string     `yaml:"timezone" json:"timezone"`
	SendWindow SendWindow `yaml:"send_window" json:"send_window"`
	Holidays   []string   `yaml:"holidays" json:"holidays"`
	// ShopifyDomain is the *.myshopify.com host that webhooks identify the shop by.
	ShopifyDomain string `yaml:"shopify_domain" json:"shopify_domain"`
	// StorefrontToken is the shop's PUBLIC Storefront API token (it ships in the storefront's JS);
	// used only to read product photos for the emails.
	StorefrontToken string `yaml:"storefront_token" json:"-"`
	// SampleItem is the line item shown in previews and test sends.
	SampleItem struct {
		Title   string `yaml:"title" json:"title"`
		Variant string `yaml:"variant" json:"variant"`
		Image   string `yaml:"image" json:"image"`
	} `yaml:"sample_item" json:"sample_item"`
}

type Trigger struct {
	On    string `yaml:"on" json:"on"`
	Step  string `yaml:"step,omitempty" json:"step,omitempty"`
	Event string `yaml:"event,omitempty" json:"event,omitempty"`
}

type Delay struct {
	Hours    float64 `yaml:"hours" json:"hours"`
	Business bool    `yaml:"business" json:"business"` // count only Mon-Fri hours
}

type Step struct {
	ID         string   `yaml:"id" json:"id"`
	Title      string   `yaml:"title" json:"title"`
	Subject    string   `yaml:"subject" json:"subject"`
	Template   string   `yaml:"template" json:"template"`
	Note       string   `yaml:"note" json:"note"`
	Status     string   `yaml:"status" json:"status"`
	Trigger    Trigger  `yaml:"trigger" json:"trigger"`
	Delay      Delay    `yaml:"delay" json:"delay"`
	Conditions []string `yaml:"conditions" json:"conditions"`
	// WindowDays bounds CondWithinWindow, counted from dispatch (step with tracking attached).
	WindowDays int `yaml:"window_days,omitempty" json:"window_days,omitempty"`
	// Immediate ignores the store send window (e.g. "out for delivery today").
	Immediate bool `yaml:"immediate,omitempty" json:"immediate,omitempty"`
}

// Sends reports whether the step may produce an email right now.
func (s Step) Sends(overrides map[string]string) bool {
	st := s.Status
	if v, ok := overrides[s.ID]; ok && st != StatusDraft {
		st = v
	}
	return st == StatusOn
}

// Timing renders the step's trigger for the panel, e.g. "30 business h after step 2.5".
func (s Step) Timing() string {
	d := ""
	if s.Delay.Hours > 0 {
		unit := "h"
		if s.Delay.Business {
			unit = "h (business)"
		} else {
			unit = "h (exact)"
		}
		d = fmt.Sprintf("%g%s ", s.Delay.Hours, unit)
	}
	switch s.Trigger.On {
	case TriggerOrderPaid:
		if d == "" {
			return "on payment"
		}
		return d + "after the sale"
	case TriggerAfterStep:
		return d + "after step " + s.Trigger.Step
	case TriggerTracking:
		return "on carrier event: " + s.Trigger.Event
	}
	return ""
}

type Flow struct {
	Store StoreCfg `yaml:"store" json:"store"`
	Steps []Step   `yaml:"steps" json:"steps"`

	Dir string `yaml:"-" json:"-"`
	loc *time.Location
	cal bizhours.Calendar
}

func (f *Flow) Step(id string) (Step, bool) {
	for _, s := range f.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

func (f *Flow) Loc() *time.Location { return f.loc }

// Dependents returns steps triggered by `after_step: id`.
func (f *Flow) Dependents(id string) []Step {
	var out []Step
	for _, s := range f.Steps {
		if s.Trigger.On == TriggerAfterStep && s.Trigger.Step == id {
			out = append(out, s)
		}
	}
	return out
}

func (f *Flow) ByTrigger(on string) []Step {
	var out []Step
	for _, s := range f.Steps {
		if s.Trigger.On == on {
			out = append(out, s)
		}
	}
	return out
}

func (f *Flow) ByEvent(ev string) []Step {
	var out []Step
	for _, s := range f.Steps {
		if s.Trigger.On == TriggerTracking && s.Trigger.Event == ev {
			out = append(out, s)
		}
	}
	return out
}

// DueAt applies the step's delay to an anchor time, then the store's send window.
func (f *Flow) DueAt(s Step, anchor time.Time) time.Time {
	t := anchor
	switch {
	case s.Delay.Hours <= 0:
	case s.Delay.Business:
		t = f.cal.Add(anchor, s.Delay.Hours)
	default:
		t = anchor.Add(time.Duration(s.Delay.Hours * float64(time.Hour)))
	}
	if !s.Immediate {
		t = bizhours.Window{Start: f.Store.SendWindow.Start, End: f.Store.SendWindow.End}.Shift(t, f.loc)
	}
	return t
}

// BusinessHoursBetween exposes the calendar for window checks.
func (f *Flow) BusinessHoursBetween(a, b time.Time) float64 { return f.cal.Between(a, b) }

func (f *Flow) validate() error {
	st := f.Store
	if st.ID == "" || st.Name == "" || st.From == "" {
		return fmt.Errorf("store.id, store.name and store.from are required")
	}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		return fmt.Errorf("store.timezone: %w", err)
	}
	f.loc = loc
	f.cal = bizhours.Calendar{Loc: loc, Holidays: map[string]bool{}}
	for _, h := range st.Holidays {
		f.cal.Holidays[h] = true
	}
	ids := map[string]bool{}
	for _, s := range f.Steps {
		if s.ID == "" {
			return fmt.Errorf("step without id")
		}
		if ids[s.ID] {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		ids[s.ID] = true
	}
	for _, s := range f.Steps {
		switch s.Status {
		case StatusOn, StatusOff, StatusDraft:
		default:
			return fmt.Errorf("step %s: status must be on|off|draft, got %q", s.ID, s.Status)
		}
		switch s.Trigger.On {
		case TriggerOrderPaid:
		case TriggerAfterStep:
			if !ids[s.Trigger.Step] || s.Trigger.Step == s.ID {
				return fmt.Errorf("step %s: trigger.step %q is unknown or self", s.ID, s.Trigger.Step)
			}
		case TriggerTracking:
			if !validEvents[s.Trigger.Event] {
				return fmt.Errorf("step %s: unknown tracking event %q", s.ID, s.Trigger.Event)
			}
		default:
			return fmt.Errorf("step %s: unknown trigger %q", s.ID, s.Trigger.On)
		}
		for _, c := range s.Conditions {
			if !validConds[c] {
				return fmt.Errorf("step %s: unknown condition %q", s.ID, c)
			}
			if c == CondWithinWindow && s.WindowDays <= 0 {
				return fmt.Errorf("step %s: within_window needs window_days", s.ID)
			}
		}
		if s.Status != StatusDraft {
			if s.Template == "" || s.Subject == "" {
				return fmt.Errorf("step %s: template and subject are required unless status is draft", s.ID)
			}
			if _, err := os.Stat(filepath.Join(f.Dir, "templates", s.Template+".html")); err != nil {
				return fmt.Errorf("step %s: %w", s.ID, err)
			}
		}
	}
	// after_step chains must not loop.
	for _, s := range f.Steps {
		seen := map[string]bool{s.ID: true}
		cur := s
		for cur.Trigger.On == TriggerAfterStep {
			if seen[cur.Trigger.Step] {
				return fmt.Errorf("step %s: after_step cycle", s.ID)
			}
			seen[cur.Trigger.Step] = true
			cur, _ = f.Step(cur.Trigger.Step)
		}
	}
	return nil
}

func Load(dir string) (*Flow, error) {
	b, err := os.ReadFile(filepath.Join(dir, "flow.yaml"))
	if err != nil {
		return nil, err
	}
	var f Flow
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	f.Dir = dir
	if err := f.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return &f, nil
}

// LoadAll loads every <root>/<store>/flow.yaml.
func LoadAll(root string) (map[string]*Flow, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "*", "flow.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string]*Flow{}
	sort.Strings(dirs)
	for _, p := range dirs {
		f, err := Load(filepath.Dir(p))
		if err != nil {
			return nil, err
		}
		if _, dup := out[f.Store.ID]; dup {
			return nil, fmt.Errorf("duplicate store id %q", f.Store.ID)
		}
		out[f.Store.ID] = f
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no stores found under %s", root)
	}
	return out, nil
}
