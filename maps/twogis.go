// Package maps provides place status verification via map services.
//
// TwoGIS checks place existence via 2GIS Catalog API.
// If a place is found, it is considered open (2GIS removes closed places).
// If not found, the caller should fall back to Yandex Maps for status check.
package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	twoGISBaseURL = "https://catalog.api.2gis.com/3.0/items"
	twoGISTimeout = 5 * time.Second
	twoGISDemoKey = "demo"
)

// TwoGISConfig configures the 2GIS checker.
type TwoGISConfig struct {
	APIKey string // default: "demo"
}

// twoGISResponse is the JSON response from 2GIS Catalog API.
type twoGISResponse struct {
	Meta struct {
		Code int `json:"code"`
	} `json:"meta"`
	Result struct {
		Items []twoGISItem `json:"items"`
		Total int          `json:"total"`
	} `json:"result"`
}

type twoGISItem struct {
	Name        string            `json:"name"`
	AddressName string            `json:"address_name"`
	Schedule    *twoGISSchedule   `json:"schedule"`
	Contacts    []twoGISContactGr `json:"contact_groups"`
}

type twoGISSchedule struct {
	Comment string `json:"comment"`
}

type twoGISContactGr struct {
	Contacts []twoGISContact `json:"contacts"`
}

type twoGISContact struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// TwoGISChecker verifies place existence via 2GIS Catalog API.
// Found → open (2GIS removes closed places from index).
// Not found → unknown (needs fallback check via Yandex Maps).
type TwoGISChecker struct {
	apiKey string
	client *http.Client
}

// NewTwoGIS creates a 2GIS checker. Empty apiKey defaults to "demo".
func NewTwoGIS(cfg TwoGISConfig) *TwoGISChecker {
	key := cfg.APIKey
	if key == "" {
		key = twoGISDemoKey
	}
	return &TwoGISChecker{
		apiKey: key,
		client: &http.Client{Timeout: twoGISTimeout},
	}
}

// Check queries 2GIS for a place. Returns PlaceOpen if found,
// PlaceNotFound if not (may be closed — needs fallback).
func (c *TwoGISChecker) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	query := name + " " + city
	params := url.Values{
		"q":      {query},
		"type":   {"branch"},
		"key":    {c.apiKey},
		"fields": {"items.schedule,items.contact_groups"},
	}

	reqURL := twoGISBaseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("2gis: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("2gis: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("2gis: read: %w", err)
	}

	var gr twoGISResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, fmt.Errorf("2gis: parse: %w", err)
	}

	if gr.Meta.Code != http.StatusOK {
		return nil, fmt.Errorf("2gis: API code %d", gr.Meta.Code)
	}

	if gr.Result.Total == 0 || len(gr.Result.Items) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	item := gr.Result.Items[0]
	result := &CheckResult{
		Status:   PlaceOpen,
		RawTitle: item.Name,
		OrgData: &OrgData{
			Status:  PlaceOpen,
			Name:    item.Name,
			Address: item.AddressName,
		},
	}

	// Extract phone and website from contacts.
	for _, cg := range item.Contacts {
		for _, ct := range cg.Contacts {
			switch ct.Type {
			case "phone":
				if result.OrgData.Phone == "" {
					result.OrgData.Phone = ct.Value
				}
			case "website":
				if result.OrgData.Website == "" {
					result.OrgData.Website = cleanTwoGISURL(ct.Value)
				}
			}
		}
	}

	if item.Schedule != nil && item.Schedule.Comment != "" {
		result.OrgData.Hours = item.Schedule.Comment
	}

	return result, nil
}

// cleanTwoGISURL extracts the real URL from a 2GIS redirect link.
// Format: http://link.2gis.ru/...?http://real-site.com/
func cleanTwoGISURL(u string) string {
	if idx := strings.Index(u, "?http"); idx >= 0 {
		return u[idx+1:]
	}
	return strings.TrimPrefix(u, "http://link.2gis.ru/")
}
