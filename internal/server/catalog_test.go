package server

import (
	"testing"

	"github.com/icl00ud/mailflow/internal/flow"
	"github.com/icl00ud/mailflow/internal/store"
)

func TestApplyCatalog(t *testing.T) {
	f := &flow.Flow{Catalog: map[string]flow.CatalogItem{
		"111": {Title: "Boutique Watch", Image: "https://site/email/w.jpg"},
		"222": {Title: "Presentation Box", Variant: "For your Boutique Watch", Image: "https://site/email/box-w.jpg"},
		"333": {Title: "Shipping Protection"},
	}}
	items := []store.Item{
		{Title: "Steel Watch – Black Dial", VariantID: 111, Qty: 1},
		{Title: "Box", Variant: "For watch: X", VariantID: 222, Qty: 1},
		{Title: "Protection", VariantID: 333, Image: "keep.jpg", Qty: 1},
		{Title: "Curren 8355", VariantID: 999, Qty: 1},
	}
	applyCatalog(f, items)
	if items[0].Title != "Boutique Watch" || items[0].Image != "https://site/email/w.jpg" {
		t.Fatalf("watch not mapped: %+v", items[0])
	}
	if items[1].Variant != "For your Boutique Watch" || items[1].Image != "https://site/email/box-w.jpg" {
		t.Fatalf("box not mapped: %+v", items[1])
	}
	if items[2].Title != "Shipping Protection" || items[2].Image != "keep.jpg" {
		t.Fatalf("add-on without image must keep the existing one: %+v", items[2])
	}
	if items[3].Title != "Curren 8355" {
		t.Fatalf("unknown variant must keep the Shopify title: %+v", items[3])
	}
}
