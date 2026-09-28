package cmd

import (
	"strings"
	"testing"
)

// Markdown image links are rendered on Windows too, so they must use forward
// slashes; a backslash both breaks the link and leaks the local separator.
func TestAssetRelPathUsesForwardSlashes(t *testing.T) {
	cases := []struct {
		name string
		img  exportImage
		want string
	}{
		{"named image", exportImage{Name: "pic.png", Mime: "image/png"}, "assets/pic.png"},
		{"name without extension", exportImage{Name: "500", Mime: "image/png"}, "assets/500.png"},
		{"fallback to the URL", exportImage{ID: "500", Mime: "image/png"}, "assets/500.png"},
	}
	for _, tc := range cases {
		got := assetRelPath(tc.img)
		if got != tc.want {
			t.Errorf("%s: expected %q, got %q", tc.name, tc.want, got)
		}
		if strings.Contains(got, `\`) {
			t.Errorf("%s: asset path must not contain a backslash: %q", tc.name, got)
		}
	}
}

func TestMarkdownImageUsesForwardSlashLink(t *testing.T) {
	got := markdownImage(exportImage{Name: "pic.png", Mime: "image/png"})
	if got != "![](assets/pic.png)" {
		t.Errorf("expected ![](assets/pic.png), got %q", got)
	}
}
