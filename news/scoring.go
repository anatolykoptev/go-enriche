package news

import (
	"strings"
)

const (
	scoreBase            = 50
	scorePositivePerWord = 5
	scorePositiveMax     = 25
	scoreMultiEngine     = 10
	scoreLongSnippet     = 5
	scoreSnippetMinLen   = 100
	scoreCityExact       = 10
	scoreCityPartial     = 5
	scoreRivalPenalty    = 5
	scoreGoodSource      = 10
	multiEngineThreshold = 1
)

// ScoreItem scores a single news item against project configuration.
// Returns 0 if the item should be rejected.
func ScoreItem(title, snippet, rawURL, source string, engineCount int, project *Project, projectKey string) int {
	if isBlockedDomain(rawURL) {
		return 0
	}
	if isHomepage(rawURL) {
		return 0
	}
	if isSelfReferencing(source, projectKey) {
		return 0
	}

	titleLower := strings.ToLower(title)
	for _, taboo := range project.TabooWords {
		if containsWordCI(titleLower, strings.ToLower(taboo)) {
			return 0
		}
	}

	score := scoreBase
	combined := titleLower + " " + strings.ToLower(snippet)
	score += scorePositiveBonus(combined, project.PositiveWords)

	if engineCount > multiEngineThreshold {
		score += scoreMultiEngine
	}
	if len(snippet) > scoreSnippetMinLen {
		score += scoreLongSnippet
	}

	score += scoreCityBonus(titleLower, project.CityNames)
	score -= scoreRivalPenalties(titleLower, project.RivalCities)
	score += scoreSourceBonus(source, project.GoodSources)

	return score
}

func scorePositiveBonus(combined string, positiveWords []string) int {
	bonus := 0
	for _, word := range positiveWords {
		if containsWordCI(combined, strings.ToLower(word)) {
			bonus += scorePositivePerWord
			if bonus >= scorePositiveMax {
				return scorePositiveMax
			}
		}
	}
	return bonus
}

func scoreCityBonus(titleLower string, cityNames []string) int {
	for _, city := range cityNames {
		cityLower := strings.ToLower(city)
		if containsWordCI(titleLower, cityLower) {
			return scoreCityExact
		}
		if strings.Contains(titleLower, cityLower) {
			return scoreCityPartial
		}
	}
	return 0
}

func scoreRivalPenalties(titleLower string, rivalCities []string) int {
	penalty := 0
	for _, rival := range rivalCities {
		if containsWordCI(titleLower, strings.ToLower(rival)) {
			penalty += scoreRivalPenalty
		}
	}
	return penalty
}

func scoreSourceBonus(source string, goodSources []string) int {
	sourceLower := strings.ToLower(source)
	for _, gs := range goodSources {
		gsLower := strings.ToLower(gs)
		if sourceLower == gsLower || strings.HasSuffix(sourceLower, "."+gsLower) {
			return scoreGoodSource
		}
	}
	return 0
}
