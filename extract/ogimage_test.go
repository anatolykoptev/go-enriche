package extract

import "testing"

func TestExtractOGImage_Found(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<meta property="og:image" content="https://example.com/photo.jpg">
	</head><body></body></html>`

	img := ExtractOGImage(html)
	if img == nil {
		t.Fatal("expected image, got nil")
	}
	if *img != "https://example.com/photo.jpg" {
		t.Errorf("expected https://example.com/photo.jpg, got %s", *img)
	}
}

func TestExtractOGImage_NotFound(t *testing.T) {
	t.Parallel()
	html := `<html><head><title>No OG</title></head><body></body></html>`

	img := ExtractOGImage(html)
	if img != nil {
		t.Errorf("expected nil, got %v", img)
	}
}

func TestExtractOGImage_Empty(t *testing.T) {
	t.Parallel()
	html := `<meta property="og:image" content="">`

	img := ExtractOGImage(html)
	if img != nil {
		t.Errorf("expected nil for empty content, got %v", img)
	}
}
