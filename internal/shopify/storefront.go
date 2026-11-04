package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/icl00ud/mailflow/internal/store"
)

// StorefrontClient fills order line-item photos from the public Storefront API.
// Webhook payloads carry no image URL, so without this the emails show text only.
var StorefrontClient = &http.Client{Timeout: 8 * time.Second}

// FillImages sets Item.Image from each line item's variant image (or the product's featured image).
// It is best effort: any error leaves the items as they are and is returned for logging only.
func FillImages(ctx context.Context, shopDomain, token string, items []store.Item) error {
	if shopDomain == "" || token == "" {
		return nil
	}
	var ids []string
	idx := map[string][]int{}
	for i, it := range items {
		if it.Image != "" || it.VariantID == 0 {
			continue
		}
		gid := "gid://shopify/ProductVariant/" + strconv.FormatInt(it.VariantID, 10)
		if _, seen := idx[gid]; !seen {
			ids = append(ids, gid)
		}
		idx[gid] = append(idx[gid], i)
	}
	if len(ids) == 0 {
		return nil
	}
	q := map[string]any{
		"query":     `query($ids:[ID!]!){ nodes(ids:$ids){ ... on ProductVariant { id image { url } product { featuredImage { url } } } } }`,
		"variables": map[string]any{"ids": ids},
	}
	body, _ := json.Marshal(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+shopDomain+"/api/2025-01/graphql.json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Storefront-Access-Token", token)
	resp, err := StorefrontClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("storefront api: %s", resp.Status)
	}
	var out struct {
		Data struct {
			Nodes []*struct {
				ID    string `json:"id"`
				Image *struct {
					URL string `json:"url"`
				} `json:"image"`
				Product struct {
					FeaturedImage *struct {
						URL string `json:"url"`
					} `json:"featuredImage"`
				} `json:"product"`
			} `json:"nodes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	for _, n := range out.Data.Nodes {
		if n == nil {
			continue
		}
		url := ""
		if n.Image != nil {
			url = n.Image.URL
		} else if n.Product.FeaturedImage != nil {
			url = n.Product.FeaturedImage.URL
		}
		if url == "" {
			continue
		}
		// Shopify CDN resizes on the fly; 240 px covers the 72 px slot on 3x screens.
		url += map[bool]string{true: "&width=240", false: "?width=240"}[bytes.ContainsRune([]byte(url), '?')]
		for _, i := range idx[n.ID] {
			items[i].Image = url
		}
	}
	return nil
}
