package search

import (
	"strings"
	"testing"
	"time"
)

func TestBuildQuery_News(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modeNews, "Go language", "")
	if query != "Go language" {
		t.Errorf("expected 'Go language', got %q", query)
	}
	if timeRange != TimeRangeWeek {
		t.Errorf("expected 'week', got %q", timeRange)
	}
}

func TestBuildQuery_Places(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modePlaces, "Cafe Nora", "Moscow")
	if query != "Cafe Nora Moscow" {
		t.Errorf("expected 'Cafe Nora Moscow', got %q", query)
	}
	if timeRange != "" {
		t.Errorf("expected empty timeRange for places, got %q", timeRange)
	}
}

func TestBuildQuery_PlacesNoCity(t *testing.T) {
	t.Parallel()
	query, _ := BuildQuery(modePlaces, "Museum", "")
	if query != "Museum" {
		t.Errorf("expected 'Museum', got %q", query)
	}
}

func TestBuildQuery_Events(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modeEvents, "Jazz Fest", "Berlin")
	year := time.Now().Format("2006")
	if !strings.Contains(query, "Jazz Fest Berlin "+year) {
		t.Errorf("expected query with city and year, got %q", query)
	}
	if timeRange != TimeRangeMonth {
		t.Errorf("expected 'month', got %q", timeRange)
	}
}

func TestBuildQuery_EventsNoCity(t *testing.T) {
	t.Parallel()
	query, _ := BuildQuery(modeEvents, "Conference", "")
	year := time.Now().Format("2006")
	if query != "Conference "+year {
		t.Errorf("expected 'Conference %s', got %q", year, query)
	}
}
