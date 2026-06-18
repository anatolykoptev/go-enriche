package extract

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// reHoursLabel matches a Russian opening-hours label that introduces a visible
// hours block: «Режим работы», «Часы работы», «Время работы» (and the bare
// «Режим»/«Часы»/«Время» when followed by работы). Case-insensitive.
var reHoursLabel = regexp.MustCompile(`(?i)(?:режим|часы|время)\s+работы`)

// reHoursValue matches a plausible opening-hours value: a day or day-range
// token optionally followed by a HH:MM-HH:MM time range, or a bare time range,
// or «ежедневно»/«круглосуточно». Used to validate that the text following a
// label actually looks like hours, not marketing prose.
var reHoursValue = regexp.MustCompile(`(?i)(?:` +
	`\d{1,2}[:.]\d{2}\s*[-–—]\s*\d{1,2}[:.]\d{2}` + // 10:00-22:00
	`|ежедневно|круглосуточно|без выходных|выходной` +
	`|(?:пн|вт|ср|чт|пт|сб|вс|пон|втор|сред|четв|пятн|субб|воскр)[а-я]*` + // day tokens
	`)`)

const (
	hoursMinLen = 4
	hoursMaxLen = 160
)

// ExtractVisibleHours reads a visible Russian opening-hours block from the DOM
// when structured data did not provide hours. It looks for an element whose
// text carries a «Режим/Часы/Время работы» label, then takes the hours value
// from the same element (or its immediate next sibling, the common
// "<dt>Часы работы</dt><dd>10:00-22:00</dd>" / label+value layout). The result
// must contain a recognizable hours value (a time range or a day token) to be
// accepted — a bare label with marketing prose is rejected.
//
// ZERO network I/O — a deterministic read over already-fetched HTML. Returns ""
// when no usable visible-hours block is found.
func ExtractVisibleHours(html string) string {
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return ""
	}

	var found string
	doc.Find("*").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		// Only consider leaf-ish elements: own text (not descendants') carrying
		// the label, so we land on the actual label node, not <body>.
		ownText := ownTextOf(s)
		if !reHoursLabel.MatchString(ownText) {
			return true
		}
		if v := hoursValueNear(s, ownText); v != "" {
			found = v
			return false
		}
		return true
	})
	return found
}

// ownTextOf returns the element's direct text content (excluding text of child
// elements), trimmed. Used so the label search lands on the node that literally
// contains «Часы работы», not every ancestor that contains it transitively.
func ownTextOf(s *goquery.Selection) string {
	var b strings.Builder
	for _, n := range s.Nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			// Only the element's OWN direct text — skip child elements so the
			// label search lands on the literal «Часы работы» node, not every
			// ancestor that transitively contains it.
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// hoursValueNear extracts a clean hours value given a label-bearing element and
// its own text. It first checks whether the label element's own text already
// carries the value inline ("Часы работы: 10:00-22:00"), then falls back to the
// element's next sibling's text (label/value split across two nodes). The value
// is validated with reHoursValue, cleaned to its hours-bearing lines (trailing
// org/legal noise dropped) and length-bounded.
func hoursValueNear(s *goquery.Selection, ownText string) string {
	// Inline: "Часы работы: 10:00-22:00" — strip the label, keep the rest.
	inline := strings.TrimSpace(reHoursLabel.ReplaceAllString(ownText, ""))
	inline = strings.TrimLeft(inline, " :—–-\t\n")
	if v := cleanHours(inline); v != "" {
		return v
	}
	// Split layout: the value lives in the next sibling element.
	sib := strings.TrimSpace(s.Next().Text())
	if v := cleanHours(sib); v != "" {
		return v
	}
	return ""
}

// reOrgNoise marks legal/identity lines a wide hours container sometimes bundles
// after the schedule (company name, tax IDs). Such a line ends the hours value.
var reOrgNoise = regexp.MustCompile(`(?i)(?:ООО|ОАО|ЗАО|ИП\b|ИНН|ОГРН|КПП|юридическ)`)

// cleanHours validates a candidate as opening hours and trims it to the
// hours-bearing lines: it keeps consecutive lines that look like a schedule
// (day token and/or time range) from the top and stops at the first line that
// is org/legal noise or carries no hours signal. Returns "" when the candidate
// is not usable hours at all.
func cleanHours(v string) string {
	if !isUsableHours(v) {
		return ""
	}
	lines := strings.Split(v, "\n")
	var kept []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			if len(kept) > 0 {
				break // blank line after the schedule ends it
			}
			continue
		}
		if reOrgNoise.MatchString(ln) || !reHoursValue.MatchString(ln) {
			if len(kept) > 0 {
				break // first non-hours line after the schedule ends it
			}
			continue // skip leading non-hours lines (e.g. a stray label)
		}
		kept = append(kept, ln)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "; ")
}

// isUsableHours reports whether a candidate string is a plausible, bounded
// opening-hours value (carries a recognizable hours token, neither empty nor
// over-long marketing prose).
func isUsableHours(v string) bool {
	v = strings.TrimSpace(v)
	rl := len([]rune(v))
	if rl < hoursMinLen || rl > hoursMaxLen {
		return false
	}
	return reHoursValue.MatchString(v)
}
