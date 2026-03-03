package news

import (
	"fmt"
	"slices"
)

// AddItems appends items to the project store, deduplicating by URL.
// Returns the count of newly added items.
func (s *Store) AddItems(project string, items []*Entry) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.load(project)
	if err != nil {
		return 0, fmt.Errorf("add items: %w", err)
	}

	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e.URL] = true
	}

	added := 0
	for _, item := range items {
		if seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		existing = append(existing, item)
		added++
	}

	if added > 0 {
		if err := s.save(project, existing); err != nil {
			return 0, fmt.Errorf("save after add: %w", err)
		}
	}
	return added, nil
}

// ListItems returns entries matching the given status filter, with score >= minScore,
// sorted by score descending and limited to limit items. Empty status means all.
func (s *Store) ListItems(project, status string, minScore, limit int) ([]*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.load(project)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}

	filtered := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if status != "" && e.Status != status {
			continue
		}
		if e.Score < minScore {
			continue
		}
		filtered = append(filtered, e)
	}

	slices.SortFunc(filtered, func(a, b *Entry) int {
		return b.Score - a.Score // descending
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// UpdateItem updates the status and/or score of the entry with the given newsID.
// A score of -1 means "leave unchanged".
func (s *Store) UpdateItem(project, newsID, status string, score int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.load(project)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}

	found := false
	for _, e := range entries {
		if e.NewsID != newsID {
			continue
		}
		if status != "" {
			e.Status = status
		}
		if score >= 0 {
			e.Score = score
		}
		found = true
		break
	}

	if !found {
		return fmt.Errorf("news_id %q not found in project %q", newsID, project)
	}

	return s.save(project, entries)
}

// ExistingURLs returns a set of all URLs already stored for a project.
func (s *Store) ExistingURLs(project string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.load(project)
	if err != nil {
		return nil, fmt.Errorf("existing URLs: %w", err)
	}

	urls := make(map[string]bool, len(entries))
	for _, e := range entries {
		urls[e.URL] = true
	}
	return urls, nil
}

// TotalCount returns the total number of entries stored for a project.
// Returns 0 on any error (best-effort counter).
func (s *Store) TotalCount(project string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.load(project)
	if err != nil {
		return 0
	}
	return len(entries)
}

// PurgeItems removes entries for which shouldPurge returns true.
// Returns the number of entries removed.
func (s *Store) PurgeItems(project string, shouldPurge func(*Entry) bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.load(project)
	if err != nil {
		return 0, fmt.Errorf("purge items: %w", err)
	}

	kept := make([]*Entry, 0, len(entries))
	removed := 0
	for _, e := range entries {
		if shouldPurge(e) {
			removed++
		} else {
			kept = append(kept, e)
		}
	}

	if removed > 0 {
		if err := s.save(project, kept); err != nil {
			return 0, fmt.Errorf("save after purge: %w", err)
		}
	}
	return removed, nil
}
