package news

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Projects: map[string]*Project{
			"piter": {
				Queries: []Query{
					{Q: "Петербург новости", Topic: "general"},
				},
				TabooWords:    []string{"реклама", "спам"},
				PositiveWords: []string{"культура", "спорт"},
				GoodSources:   []string{"fontanka.ru"},
				CityNames:     []string{"Петербург", "Санкт-Петербург"},
				RivalCities:   []string{"Москва"},
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal test config: %v", err)
	}

	path := filepath.Join(t.TempDir(), "news-config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got == nil {
		t.Fatal("LoadConfig() returned nil config")
	}
	if len(got.Projects) != 1 {
		t.Fatalf("Projects count = %d, want 1", len(got.Projects))
	}

	proj, ok := got.Projects["piter"]
	if !ok {
		t.Fatal("Projects[\"piter\"] not found")
	}
	if len(proj.Queries) != 1 {
		t.Errorf("Queries count = %d, want 1", len(proj.Queries))
	}
	if proj.Queries[0].Q != "Петербург новости" {
		t.Errorf("Queries[0].Q = %q, want %q", proj.Queries[0].Q, "Петербург новости")
	}
	if len(proj.TabooWords) != 2 {
		t.Errorf("TabooWords count = %d, want 2", len(proj.TabooWords))
	}
	if len(proj.CityNames) != 2 {
		t.Errorf("CityNames count = %d, want 2", len(proj.CityNames))
	}
	if len(proj.RivalCities) != 1 {
		t.Errorf("RivalCities count = %d, want 1", len(proj.RivalCities))
	}
}

func TestLoadConfig_FileNotExist(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("LoadConfig() on missing file should return error, got nil")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{not valid json`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() on invalid JSON should return error, got nil")
	}
}
