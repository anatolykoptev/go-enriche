package news

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// makeEntry creates a minimal Entry for use in tests.
func makeEntry(newsID, url, status string, score int) *Entry {
	return &Entry{
		NewsID:  newsID,
		Project: "testproject",
		URL:     url,
		Title:   "Title " + newsID,
		Status:  status,
		Score:   score,
	}
}

func TestStore_SaveLoad(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	entries := []*Entry{
		makeEntry("id1", "https://example.com/1", "pending", 60),
		makeEntry("id2", "https://example.com/2", "selected", 75),
	}

	if err := store.Save("proj", entries); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load("proj")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("Load() returned %d entries, want %d", len(got), len(entries))
	}
	for i, e := range got {
		if e.NewsID != entries[i].NewsID {
			t.Errorf("entry[%d].NewsID = %q, want %q", i, e.NewsID, entries[i].NewsID)
		}
		if e.URL != entries[i].URL {
			t.Errorf("entry[%d].URL = %q, want %q", i, e.URL, entries[i].URL)
		}
		if e.Score != entries[i].Score {
			t.Errorf("entry[%d].Score = %d, want %d", i, e.Score, entries[i].Score)
		}
	}
}

func TestStore_LoadNonExistent(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())

	got, err := store.Load("nonexistent")
	if err != nil {
		t.Fatalf("Load() on non-existent project should not error, got: %v", err)
	}
	if got == nil {
		t.Fatal("Load() returned nil, want empty (non-nil) slice")
	}
	if len(got) != 0 {
		t.Fatalf("Load() returned %d entries, want 0", len(got))
	}
}

func TestStore_LoadEnvelopeFormat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)

	// Write manually in envelope format (legacy Vaelor).
	newsDir := filepath.Join(dir, "news")
	if err := os.MkdirAll(newsDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	envelope := storeEnvelope{
		Items: []*Entry{
			makeEntry("env1", "https://example.com/env/1", "pending", 55),
			makeEntry("env2", "https://example.com/env/2", "pending", 65),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal envelope: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newsDir, "envproj.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := store.Load("envproj")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load() returned %d entries, want 2", len(got))
	}
	if got[0].NewsID != "env1" {
		t.Errorf("got[0].NewsID = %q, want %q", got[0].NewsID, "env1")
	}
	if got[1].NewsID != "env2" {
		t.Errorf("got[1].NewsID = %q, want %q", got[1].NewsID, "env2")
	}
}

func TestStore_AddItems(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	items := []*Entry{
		makeEntry("a1", "https://example.com/a/1", "pending", 50),
		makeEntry("a2", "https://example.com/a/2", "pending", 60),
	}

	n, err := store.AddItems("proj", items)
	if err != nil {
		t.Fatalf("AddItems() error = %v", err)
	}
	if n != 2 {
		t.Errorf("AddItems() added %d, want 2", n)
	}

	loaded, err := store.Load("proj")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("Load() returned %d entries after AddItems, want 2", len(loaded))
	}
}

func TestStore_AddItems_NoDuplicates(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	items := []*Entry{
		makeEntry("dup1", "https://example.com/dup/1", "pending", 50),
	}

	if _, err := store.AddItems("proj", items); err != nil {
		t.Fatalf("first AddItems() error = %v", err)
	}

	// Adding same URLs again should add zero new entries.
	n, err := store.AddItems("proj", items)
	if err != nil {
		t.Fatalf("second AddItems() error = %v", err)
	}
	if n != 0 {
		t.Errorf("AddItems() with duplicates added %d, want 0", n)
	}

	loaded, err := store.Load("proj")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("store has %d entries after dedup, want 1", len(loaded))
	}
}

func TestStore_ListItems(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	entries := []*Entry{
		makeEntry("l1", "https://example.com/l/1", "pending", 80),
		makeEntry("l2", "https://example.com/l/2", "selected", 70),
		makeEntry("l3", "https://example.com/l/3", "pending", 60),
		makeEntry("l4", "https://example.com/l/4", "rejected", 40),
	}
	if err := store.Save("proj", entries); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tests := []struct {
		name      string
		status    string
		minScore  int
		limit     int
		wantCount int
		wantIDs   []string // in score-descending order
	}{
		{
			name:      "filter by status pending",
			status:    "pending",
			minScore:  0,
			limit:     0,
			wantCount: 2,
			wantIDs:   []string{"l1", "l3"},
		},
		{
			name:      "filter by minScore",
			status:    "",
			minScore:  65,
			limit:     0,
			wantCount: 2,
			wantIDs:   []string{"l1", "l2"},
		},
		{
			name:      "limit results",
			status:    "",
			minScore:  0,
			limit:     2,
			wantCount: 2,
			wantIDs:   []string{"l1", "l2"},
		},
		{
			name:      "sorted descending by score",
			status:    "",
			minScore:  0,
			limit:     0,
			wantCount: 4,
			wantIDs:   []string{"l1", "l2", "l3", "l4"},
		},
		{
			name:      "no results when status not present",
			status:    "published",
			minScore:  0,
			limit:     0,
			wantCount: 0,
			wantIDs:   []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := store.ListItems("proj", tc.status, tc.minScore, tc.limit)
			if err != nil {
				t.Fatalf("ListItems() error = %v", err)
			}
			if len(got) != tc.wantCount {
				t.Fatalf("ListItems() returned %d entries, want %d", len(got), tc.wantCount)
			}
			for i, id := range tc.wantIDs {
				if got[i].NewsID != id {
					t.Errorf("result[%d].NewsID = %q, want %q", i, got[i].NewsID, id)
				}
			}
			// Verify descending score order.
			for i := 1; i < len(got); i++ {
				if got[i].Score > got[i-1].Score {
					t.Errorf("results not sorted descending: got[%d].Score=%d > got[%d].Score=%d",
						i, got[i].Score, i-1, got[i-1].Score)
				}
			}
		})
	}
}

func TestStore_UpdateItem(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	entries := []*Entry{
		makeEntry("u1", "https://example.com/u/1", "pending", 55),
	}
	if err := store.Save("proj", entries); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	t.Run("update status and score", func(t *testing.T) {
		if err := store.UpdateItem("proj", "u1", "selected", 90); err != nil {
			t.Fatalf("UpdateItem() error = %v", err)
		}
		loaded, err := store.Load("proj")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if loaded[0].Status != "selected" {
			t.Errorf("Status = %q, want %q", loaded[0].Status, "selected")
		}
		if loaded[0].Score != 90 {
			t.Errorf("Score = %d, want 90", loaded[0].Score)
		}
	})

	t.Run("score minus one leaves score unchanged", func(t *testing.T) {
		// Set a known score first.
		if err := store.UpdateItem("proj", "u1", "pending", 77); err != nil {
			t.Fatalf("setup UpdateItem() error = %v", err)
		}
		if err := store.UpdateItem("proj", "u1", "selected", -1); err != nil {
			t.Fatalf("UpdateItem() with score=-1 error = %v", err)
		}
		loaded, err := store.Load("proj")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if loaded[0].Score != 77 {
			t.Errorf("Score = %d after score=-1 update, want 77 (unchanged)", loaded[0].Score)
		}
	})

	t.Run("not-found returns error", func(t *testing.T) {
		err := store.UpdateItem("proj", "nonexistent-id", "selected", 50)
		if err == nil {
			t.Fatal("UpdateItem() with unknown newsID should return error, got nil")
		}
	})
}

func TestStore_PurgeItems(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	entries := []*Entry{
		makeEntry("p1", "https://example.com/p/1", "rejected", 30),
		makeEntry("p2", "https://example.com/p/2", "pending", 50),
		makeEntry("p3", "https://example.com/p/3", "rejected", 20),
	}
	if err := store.Save("proj", entries); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	removed, err := store.PurgeItems("proj", func(e *Entry) bool {
		return e.Status == "rejected"
	})
	if err != nil {
		t.Fatalf("PurgeItems() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("PurgeItems() removed %d, want 2", removed)
	}

	loaded, err := store.Load("proj")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("store has %d entries after purge, want 1", len(loaded))
	}
	if loaded[0].NewsID != "p2" {
		t.Errorf("remaining entry NewsID = %q, want %q", loaded[0].NewsID, "p2")
	}
}

func TestStore_ExistingURLs(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	entries := []*Entry{
		makeEntry("e1", "https://example.com/e/1", "pending", 50),
		makeEntry("e2", "https://example.com/e/2", "pending", 60),
	}
	if err := store.Save("proj", entries); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	urls, err := store.ExistingURLs("proj")
	if err != nil {
		t.Fatalf("ExistingURLs() error = %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("ExistingURLs() returned %d URLs, want 2", len(urls))
	}
	for _, e := range entries {
		if !urls[e.URL] {
			t.Errorf("ExistingURLs() missing URL %q", e.URL)
		}
	}
}

func TestStore_TotalCount(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())

	t.Run("returns zero for non-existent project", func(t *testing.T) {
		n := store.TotalCount("ghost")
		if n != 0 {
			t.Errorf("TotalCount() = %d, want 0", n)
		}
	})

	t.Run("returns correct count after save", func(t *testing.T) {
		entries := []*Entry{
			makeEntry("tc1", "https://example.com/tc/1", "pending", 50),
			makeEntry("tc2", "https://example.com/tc/2", "pending", 60),
			makeEntry("tc3", "https://example.com/tc/3", "pending", 70),
		}
		if err := store.Save("tcproj", entries); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		n := store.TotalCount("tcproj")
		if n != 3 {
			t.Errorf("TotalCount() = %d, want 3", n)
		}
	})
}
