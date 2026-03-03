package news

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Entry represents a single news item discovered by the pipeline.
type Entry struct {
	NewsID       string `json:"news_id"`
	Project      string `json:"project"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	Source       string `json:"source"`
	Topic        string `json:"topic"`
	Score        int    `json:"score"`
	EngineCount  int    `json:"engine_count"`
	DiscoveredAt string `json:"discovered_at"`
	Status       string `json:"status"` // pending, selected, published, rejected
}

// Store is a file-based JSON store for news entries, one file per project.
// Files are stored at {dir}/{project}.json.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore creates a Store rooted at {stateDir}/news.
func NewStore(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, "news")}
}

// storeEnvelope is the legacy Vaelor format: {"items": [...], "updated_at": "..."}.
type storeEnvelope struct {
	Items []*Entry `json:"items"`
}

// Load reads all entries for a project from disk.
func (s *Store) Load(project string) ([]*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.load(project)
}

// load is the unlocked internal read — caller must hold at least an RLock.
// Supports both bare JSON arrays and the legacy envelope format.
func (s *Store) load(project string) ([]*Entry, error) {
	path := s.path(project)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []*Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read news store %q: %w", path, err)
	}

	// Try bare array first (go-wp native format).
	var entries []*Entry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}

	// Try envelope format (legacy Vaelor).
	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse news store %q: %w", path, err)
	}
	return env.Items, nil
}

// Save writes entries for a project to disk atomically (write to temp, rename).
func (s *Store) Save(project string, entries []*Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(project, entries)
}

// save is the unlocked internal write — caller must hold a Lock.
func (s *Store) save(project string, entries []*Entry) error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil { //nolint:mnd
		return fmt.Errorf("create news dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal news entries: %w", err)
	}
	path := s.path(project)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:mnd
		return fmt.Errorf("write news store temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename news store: %w", err)
	}
	return nil
}

// path returns the full file path for a project's store file.
func (s *Store) path(project string) string {
	return filepath.Join(s.dir, project+".json")
}
