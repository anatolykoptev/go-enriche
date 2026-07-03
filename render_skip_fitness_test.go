package enriche

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFitness_RenderSkipCompositeSingleDefinition is the ADR-4 anti-fab fitness
// function: the render-skip composite — a poison-negation combined with
// hasAnchoredSiteNumber — MUST live in EXACTLY ONE function definition
// (rawContactsSufficient). Both fetch legs (fetchAndExtract's homepage render
// and fetchContactsHTML's contacts render) gate the render on that one
// predicate, so they can never diverge and let a laundered number through one
// leg while the other blocks it. If a future edit re-inlines the
// "!poisoned && hasAnchoredSiteNumber(...)" composite at either gate, the hit
// count becomes 2 and this test goes RED — divergence is structurally caught,
// not merely discouraged.
func TestFitness_RenderSkipCompositeSingleDefinition(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}

	var hits []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // fitness scans production source only
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		curFn := ""
		for i, raw := range strings.Split(string(src), "\n") {
			code := fitnessStripComment(raw) // drop // comments so doc prose never matches
			if fn := fitnessFuncName(code); fn != "" {
				curFn = fn
			}
			if strings.Contains(code, "hasAnchoredSiteNumber") &&
				strings.Contains(strings.ToLower(code), "poison") {
				hits = append(hits, fmt.Sprintf("%s:%d in %s", f, i+1, curFn))
			}
		}
	}

	if len(hits) != 1 {
		t.Fatalf("render-skip composite (poison + hasAnchoredSiteNumber) must appear in EXACTLY ONE place; got %d: %v", len(hits), hits)
	}
	if !strings.Contains(hits[0], "rawContactsSufficient") {
		t.Fatalf("render-skip composite must live in rawContactsSufficient; found in: %s", hits[0])
	}
}

// fitnessStripComment returns line with any // line-comment removed. (No source
// line in this package carries "//" inside a string literal, so a plain cut is
// safe here — this is a grep fitness test, not a full lexer.)
func fitnessStripComment(line string) string {
	if idx := strings.Index(line, "//"); idx != -1 {
		return line[:idx]
	}
	return line
}

// fitnessFuncName returns the function name declared on line, or "" if line is
// not a func declaration. Handles both plain funcs and methods with a receiver.
func fitnessFuncName(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "func ") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "func "))
	if strings.HasPrefix(rest, "(") { // skip a "(recv T)" receiver
		if idx := strings.Index(rest, ")"); idx != -1 {
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}
	if end := strings.IndexAny(rest, "( "); end != -1 {
		return rest[:end]
	}
	return rest
}
