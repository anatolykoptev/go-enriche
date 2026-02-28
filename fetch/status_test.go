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
