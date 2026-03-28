package maps

import (
	"context"
	"log/slog"
)

// CompositeChecker tries multiple checkers in order.
// First checker that returns a definitive status (not PlaceNotFound) wins.
// If all return PlaceNotFound, the last NotFound result is returned.
type CompositeChecker struct {
	checkers []namedChecker
}

type namedChecker struct {
	name    string
	checker Checker
}

// NewCompositeChecker creates a checker that tries multiple backends in order.
func NewCompositeChecker(checkers ...NamedChecker) *CompositeChecker {
	named := make([]namedChecker, len(checkers))
	for i, c := range checkers {
		named[i] = namedChecker{name: c.Name, checker: c.Checker}
	}
	return &CompositeChecker{checkers: named}
}

// NamedChecker pairs a checker with a name for logging.
type NamedChecker struct {
	Name    string
	Checker Checker
}

// Check tries each checker in order until one returns a definitive status.
func (c *CompositeChecker) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	var lastResult *CheckResult

	for _, nc := range c.checkers {
		result, err := nc.checker.Check(ctx, name, city)
		if err != nil {
			slog.Debug("composite checker: backend failed",
				slog.String("backend", nc.name),
				slog.String("place", name),
				slog.Any("error", err))
			continue
		}

		// Definitive answer — return immediately.
		if result.Status != PlaceNotFound {
			return result, nil
		}

		lastResult = result
	}

	if lastResult != nil {
		return lastResult, nil
	}
	return &CheckResult{Status: PlaceNotFound}, nil
}
