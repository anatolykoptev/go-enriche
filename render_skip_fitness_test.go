package enriche

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compositeWindow is how many source lines on each side of a hasAnchoredSiteNumber
// occurrence the fitness matcher joins before checking for a poison token. It must
// exceed the largest gap gofmt can put between the two halves of a wrapped boolean
// expression (a couple of lines), so a re-inlined composite that gofmt wraps across
// lines can never evade the guard by landing the two substrings on separate lines.
const compositeWindow = 2

// TestFitness_RenderSkipCompositeSingleDefinition is the ADR-4 anti-fab fitness
// function: the render-skip composite — a poison-negation combined with
// hasAnchoredSiteNumber — MUST live in EXACTLY ONE function definition
// (rawContactsSufficient). Both fetch legs (fetchAndExtract's homepage render and
// fetchContactsHTML's contacts render) gate the render on that one predicate, so
// they can never diverge and let a laundered number through one leg while the
// other blocks it. If a future edit re-inlines the "!poisoned && hasAnchored..."
// composite at either gate — even if gofmt WRAPS it across lines — the hit count
// becomes 2 and this test goes RED. Divergence is structurally caught, not merely
// discouraged.
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
		hits = append(hits, findRenderSkipComposite(f, src)...)
	}

	if len(hits) != 1 {
		t.Fatalf("render-skip composite (poison + hasAnchoredSiteNumber within a %d-line window) must appear in EXACTLY ONE place; got %d: %v", compositeWindow, len(hits), hits)
	}
	if !strings.Contains(hits[0], "rawContactsSufficient") {
		t.Fatalf("render-skip composite must live in rawContactsSufficient; found in: %s", hits[0])
	}
}

// TestFitness_WindowCatchesWrappedComposite proves MEDIUM#4: the windowed matcher
// catches a gofmt-WRAPPED re-inlining (the poison gate and hasAnchoredSiteNumber on
// SEPARATE lines — exactly what a naive per-line matcher would miss), and does NOT
// false-positive on a lone hasAnchoredSiteNumber with no poison nearby (the
// homepageMissingRichField shape).
func TestFitness_WindowCatchesWrappedComposite(t *testing.T) {
	t.Parallel()
	// A gofmt-wrapped re-inlining: !homeRawPoisoned and hasAnchoredSiteNumber land
	// on adjacent-but-separate lines. A per-line Contains would MISS it.
	wrapped := []byte("package x\n\nfunc evilInlinedGate() bool {\n\treturn browserFetch != nil &&\n\t\t!homeRawPoisoned &&\n\t\thasAnchoredSiteNumber(rawSiteNumbers)\n}\n")
	if hits := findRenderSkipComposite("wrapped.go", wrapped); len(hits) != 1 {
		t.Fatalf("windowed matcher must catch a gofmt-wrapped composite (poison + anchored on separate lines); got %d: %v", len(hits), hits)
	}
	// A lone hasAnchoredSiteNumber with no poison nearby (homepageMissingRichField
	// shape) must NOT be a hit.
	clean := []byte("package x\n\nfunc missingField(nums []T) bool {\n\treturn a == nil || b == nil || !hasAnchoredSiteNumber(nums)\n}\n")
	if hits := findRenderSkipComposite("clean.go", clean); len(hits) != 0 {
		t.Fatalf("a lone hasAnchoredSiteNumber with no poison nearby must NOT hit; got %v", hits)
	}
	// A doc COMMENT mentioning both tokens must NOT hit (comments are stripped).
	commented := []byte("package x\n\n// poison and hasAnchoredSiteNumber discussed in prose\nfunc doc() {}\n")
	if hits := findRenderSkipComposite("commented.go", commented); len(hits) != 0 {
		t.Fatalf("a comment mentioning both tokens must NOT hit; got %v", hits)
	}
}

// findRenderSkipComposite returns a descriptor for each hasAnchoredSiteNumber
// occurrence (in comment-stripped code) that has a "poison" token within a
// ±compositeWindow line window — i.e. each place the render-skip composite lives,
// robust to gofmt wrapping the boolean across lines. Comments are stripped first so
// doc prose discussing the composite never matches.
func findRenderSkipComposite(label string, src []byte) []string {
	lines := strings.Split(string(src), "\n")
	code := make([]string, len(lines))
	fns := make([]string, len(lines))
	cur := ""
	for i, raw := range lines {
		c := fitnessStripComment(raw)
		code[i] = c
		if fn := fitnessFuncName(c); fn != "" {
			cur = fn
		}
		fns[i] = cur
	}

	var hits []string
	for i, c := range code {
		if !strings.Contains(c, "hasAnchoredSiteNumber") {
			continue
		}
		lo, hi := i-compositeWindow, i+compositeWindow+1
		if lo < 0 {
			lo = 0
		}
		if hi > len(code) {
			hi = len(code)
		}
		window := strings.ToLower(strings.Join(code[lo:hi], "\n"))
		if strings.Contains(window, "poison") {
			hits = append(hits, fmt.Sprintf("%s:%d in %s", label, i+1, fns[i]))
		}
	}
	return hits
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

// fitnessFuncName returns the function name declared on line, or "" if line is not
// a func declaration. Handles both plain funcs and methods with a receiver.
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
