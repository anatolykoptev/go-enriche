package fetch

import "testing"

func TestPageStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status PageStatus
		want   string
	}{
		{StatusActive, "active"},
		{StatusNotFound, "not_found"},
		{StatusRedirect, "redirect"},
		{StatusUnreachable, "unreachable"},
		{StatusWebsiteDown, "website_down"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("PageStatus %q != %q", tt.status, tt.want)
		}
	}
}

func TestFetchResult_Zero(t *testing.T) {
	t.Parallel()
	var r FetchResult
	if r.Status != "" || r.HTML != "" || r.FinalURL != "" || r.StatusCode != 0 {
		t.Error("zero FetchResult should have empty fields")
	}
}

func TestFetchResult_IsTransient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result FetchResult
		want   bool
	}{
		{"503", FetchResult{Status: StatusUnreachable, StatusCode: 503}, true},
		{"502", FetchResult{Status: StatusUnreachable, StatusCode: 502}, true},
		{"504", FetchResult{Status: StatusUnreachable, StatusCode: 504}, true},
		{"429", FetchResult{Status: StatusUnreachable, StatusCode: 429}, true},
		{"0-connection-fail", FetchResult{Status: StatusUnreachable, StatusCode: 0}, true},
		{"404", FetchResult{Status: StatusNotFound, StatusCode: 404}, false},
		{"200", FetchResult{Status: StatusActive, StatusCode: 200}, false},
		{"redirect", FetchResult{Status: StatusRedirect, StatusCode: 301}, false},
		{"403-not-transient", FetchResult{Status: StatusUnreachable, StatusCode: 403}, false},
	}
	for _, tt := range tests {
		if got := tt.result.IsTransient(); got != tt.want {
			t.Errorf("%s: IsTransient() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
