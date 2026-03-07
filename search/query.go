package search

import (
	"fmt"
	"time"
)

// Time range constants for search providers.
const (
	TimeRangeDay   = "day"
	TimeRangeWeek  = "week"
	TimeRangeMonth = "month"
	TimeRangeYear  = "year"
)

// Mode constants matching root enriche.Mode values.
const (
	modeNews   = 0
	modePlaces = 1
	modeEvents = 2
)

// BuildQuery constructs a search query and time range based on enrichment mode.
// mode: 0=news, 1=places, 2=events (matches enriche.Mode iota values).
func BuildQuery(mode int, name, city string) (query, timeRange string) {
	switch mode {
	case modePlaces:
		if city != "" {
			return fmt.Sprintf("%s %s", name, city), ""
		}
		return name, ""
	case modeEvents:
		year := time.Now().Format("2006")
		if city != "" {
			return fmt.Sprintf("%s %s %s", name, city, year), TimeRangeMonth
		}
		return fmt.Sprintf("%s %s", name, year), TimeRangeMonth
	default: // news
		return name, TimeRangeWeek
	}
}
