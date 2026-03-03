package news

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level news configuration loaded from news-config.json.
type Config struct {
	Projects map[string]*Project `json:"projects"`
}

// Project holds per-project news discovery configuration.
type Project struct {
	Queries       []Query  `json:"queries"`
	TabooWords    []string `json:"taboo_words"`
	PositiveWords []string `json:"positive_words"`
	GoodSources   []string `json:"good_sources"`
	CityNames     []string `json:"city_names"`
	RivalCities   []string `json:"rival_cities"`
}

// Query is a single SearXNG search query configuration.
type Query struct {
	Q          string `json:"q"`
	Topic      string `json:"topic"`
	Engines    string `json:"engines,omitempty"`
	Categories string `json:"categories,omitempty"`
	TimeRange  string `json:"time_range,omitempty"`
}

// LoadConfig reads and unmarshals a news config JSON file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read news config %q: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse news config %q: %w", path, err)
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]*Project)
	}
	return &cfg, nil
}
