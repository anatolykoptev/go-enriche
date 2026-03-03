package news

import (
	"strings"
	"testing"
)

// newProject is a convenience constructor for test fixtures.
func newProject(taboo, positive, cities, rivals []string) *Project {
	return &Project{
		TabooWords:    taboo,
		PositiveWords: positive,
		CityNames:     cities,
		RivalCities:   rivals,
	}
}

var baseProject = newProject(nil, nil, nil, nil)

var scoreItemTests = []struct {
	name        string
	title       string
	snippet     string
	rawURL      string
	source      string
	engineCount int
	project     *Project
	projectKey  string
	want        int
}{
	{
		name:        "blocked domain returns zero",
		title:       "Some news",
		snippet:     "Some snippet",
		rawURL:      "https://vk.com/news/123",
		source:      "vk.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "blocked subdomain returns zero",
		title:       "Some news",
		snippet:     "Some snippet",
		rawURL:      "https://m.vk.com/news/123",
		source:      "m.vk.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "homepage path slash returns zero",
		title:       "Some news",
		snippet:     "Some snippet",
		rawURL:      "https://example.com/",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "homepage empty path returns zero",
		title:       "Some news",
		snippet:     "Some snippet",
		rawURL:      "https://example.com",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "self-referencing source returns zero",
		title:       "Some news",
		snippet:     "Some snippet",
		rawURL:      "https://mysite.ru/article/1",
		source:      "mysite.ru",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "taboo word in title returns zero",
		title:       "Реклама нового продукта",
		snippet:     "Some snippet",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject([]string{"реклама"}, nil, nil, nil),
		projectKey:  "mysite",
		want:        0,
	},
	{
		name:        "base score is 50",
		title:       "Neutral headline",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        scoreBase,
	},
	{
		name:        "one positive word adds 5",
		title:       "Хорошие новости",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject(nil, []string{"хорошие"}, nil, nil),
		projectKey:  "mysite",
		want:        scoreBase + scorePositivePerWord,
	},
	{
		name:        "positive word bonus caps at 25",
		title:       "один два три четыре пять шесть",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject(nil, []string{"один", "два", "три", "четыре", "пять", "шесть"}, nil, nil),
		projectKey:  "mysite",
		want:        scoreBase + scorePositiveMax,
	},
	{
		name:        "multi-engine bonus adds 10 when engineCount greater than 1",
		title:       "Neutral headline",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 2,
		project:     baseProject,
		projectKey:  "mysite",
		want:        scoreBase + scoreMultiEngine,
	},
	{
		name:        "no multi-engine bonus when engineCount equals 1",
		title:       "Neutral headline",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        scoreBase,
	},
	{
		name:        "long snippet adds 5",
		title:       "Neutral headline",
		snippet:     strings.Repeat("a", scoreSnippetMinLen+1),
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        scoreBase + scoreLongSnippet,
	},
	{
		name:        "snippet exactly at boundary does not add bonus",
		title:       "Neutral headline",
		snippet:     strings.Repeat("a", scoreSnippetMinLen),
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     baseProject,
		projectKey:  "mysite",
		want:        scoreBase,
	},
	{
		name:        "city exact word match adds 10",
		title:       "Петербург встречает гостей",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject(nil, nil, []string{"Петербург"}, nil),
		projectKey:  "mysite",
		want:        scoreBase + scoreCityExact,
	},
	{
		name:        "city partial substring match adds 5",
		title:       "Петербургские улицы",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject(nil, nil, []string{"Петербург"}, nil),
		projectKey:  "mysite",
		want:        scoreBase + scoreCityPartial,
	},
	{
		name:        "rival city subtracts 5",
		title:       "Москва лидирует",
		snippet:     "Short.",
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 1,
		project:     newProject(nil, nil, nil, []string{"Москва"}),
		projectKey:  "mysite",
		want:        scoreBase - scoreRivalPenalty,
	},
	{
		name:        "all bonuses stacked",
		title:       "Петербург хорошие новости важный день",
		snippet:     strings.Repeat("x", scoreSnippetMinLen+1),
		rawURL:      "https://example.com/article/1",
		source:      "example.com",
		engineCount: 3,
		project: newProject(
			nil,
			[]string{"хорошие", "важный"},
			[]string{"Петербург"},
			nil,
		),
		projectKey: "mysite",
		// base=50 + positive=10 + multiEngine=10 + longSnippet=5 + cityExact=10 = 85
		want: scoreBase + 2*scorePositivePerWord + scoreMultiEngine + scoreLongSnippet + scoreCityExact,
	},
}

func TestScoreItem(t *testing.T) {
	t.Parallel()

	for _, tc := range scoreItemTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ScoreItem(tc.title, tc.snippet, tc.rawURL, tc.source, tc.engineCount, tc.project, tc.projectKey)
			if got != tc.want {
				t.Errorf("ScoreItem() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestContainsWordCI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		word string
		want bool
	}{
		{
			name: "exact match single word",
			text: "hello",
			word: "hello",
			want: true,
		},
		{
			name: "word at start of text",
			text: "hello world",
			word: "hello",
			want: true,
		},
		{
			name: "word at end of text",
			text: "say hello",
			word: "hello",
			want: true,
		},
		{
			name: "word in middle",
			text: "say hello world",
			word: "hello",
			want: true,
		},
		{
			name: "partial match rejected — word is prefix of longer word",
			text: "петербургские улицы",
			word: "петер",
			want: false,
		},
		{
			name: "partial match rejected — word is suffix of longer word",
			text: "санкт-петербург",
			word: "бург",
			want: false,
		},
		{
			name: "Russian whole word match",
			text: "петербург встречает гостей",
			word: "петербург",
			want: true,
		},
		{
			name: "Russian word not a whole word",
			text: "петербургские новости",
			word: "петербург",
			want: false,
		},
		{
			name: "empty word returns false",
			text: "anything",
			word: "",
			want: false,
		},
		{
			name: "empty text returns false",
			text: "",
			word: "word",
			want: false,
		},
		{
			name: "word separated by punctuation boundary",
			text: "hello,world",
			word: "world",
			want: true,
		},
		{
			name: "word separated by space on both sides",
			text: "one two three",
			word: "two",
			want: true,
		},
		{
			name: "word not present at all",
			text: "fox jumps over",
			word: "dog",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := containsWordCI(tc.text, tc.word)
			if got != tc.want {
				t.Errorf("containsWordCI(%q, %q) = %v, want %v", tc.text, tc.word, got, tc.want)
			}
		})
	}
}

func TestIsBlockedDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{
			name:   "direct blocked domain",
			rawURL: "https://vk.com/news/123",
			want:   true,
		},
		{
			name:   "subdomain of blocked domain",
			rawURL: "https://m.vk.com/news/123",
			want:   true,
		},
		{
			name:   "deep subdomain of blocked domain",
			rawURL: "https://news.google.com/article",
			want:   true,
		},
		{
			name:   "non-blocked domain",
			rawURL: "https://example.com/article/1",
			want:   false,
		},
		{
			name:   "malformed URL returns false",
			rawURL: "://not a valid url",
			want:   false,
		},
		{
			name:   "youtube blocked",
			rawURL: "https://youtube.com/watch?v=abc",
			want:   true,
		},
		{
			name:   "domain that merely contains blocked name is not blocked",
			rawURL: "https://notvkcom.example.com/article",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isBlockedDomain(tc.rawURL)
			if got != tc.want {
				t.Errorf("isBlockedDomain(%q) = %v, want %v", tc.rawURL, got, tc.want)
			}
		})
	}
}

func TestIsHomepage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{
			name:   "path is slash",
			rawURL: "https://example.com/",
			want:   true,
		},
		{
			name:   "no path component",
			rawURL: "https://example.com",
			want:   true,
		},
		{
			name:   "article path is not homepage",
			rawURL: "https://example.com/article/123",
			want:   false,
		},
		{
			name:   "path with query string is not homepage",
			rawURL: "https://example.com/search?q=news",
			want:   false,
		},
		{
			name:   "single segment path is not homepage",
			rawURL: "https://example.com/about",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isHomepage(tc.rawURL)
			if got != tc.want {
				t.Errorf("isHomepage(%q) = %v, want %v", tc.rawURL, got, tc.want)
			}
		})
	}
}

func TestIsSelfReferencing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		projectKey string
		want       bool
	}{
		{
			name:       "source contains projectKey",
			source:     "mysite.ru",
			projectKey: "mysite",
			want:       true,
		},
		{
			name:       "projectKey contains source",
			source:     "site",
			projectKey: "mysite.ru",
			want:       true,
		},
		{
			name:       "no overlap",
			source:     "other.com",
			projectKey: "mysite",
			want:       false,
		},
		{
			name:       "empty source returns false",
			source:     "",
			projectKey: "mysite",
			want:       false,
		},
		{
			name:       "empty projectKey returns false",
			source:     "mysite.ru",
			projectKey: "",
			want:       false,
		},
		{
			name:       "both empty returns false",
			source:     "",
			projectKey: "",
			want:       false,
		},
		{
			name:       "case-insensitive match",
			source:     "MySite.RU",
			projectKey: "mysite",
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isSelfReferencing(tc.source, tc.projectKey)
			if got != tc.want {
				t.Errorf("isSelfReferencing(%q, %q) = %v, want %v", tc.source, tc.projectKey, got, tc.want)
			}
		})
	}
}
