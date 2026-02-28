package extract

import imagefy "github.com/anatolykoptev/go-imagefy"

// ExtractOGImage extracts the og:image URL from HTML.
// Returns nil if not found or empty.
func ExtractOGImage(html string) *string {
	img := imagefy.ExtractOGImageURL(html)
	if img == "" {
		return nil
	}
	return &img
}
