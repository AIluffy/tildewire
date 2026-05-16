package githubreadme

import (
	"strings"
	"testing"

	"github.com/zhangxueai/tildewire/internal/domain"
)

func TestCleanRemovesBadgesAndKeepsReadmeContent(t *testing.T) {
	cleaned := Clean(strings.Join([]string{
		"[![Rust 1.85+](https://img.shields.io/badge/rust-1.85+-orange.svg)](https://www.rust-lang.org/)",
		"",
		"# owner/repo",
		"",
		"![Pose fusion demo](docs/pose-fusion.png)",
		"",
		"> | What | How | Speed | > |------|-----|-------| > | Pose estimation | CSI | 171K emb/s |",
	}, "\n"))

	for _, want := range []string{"# owner/repo", "![Pose fusion demo](docs/pose-fusion.png)", "| What | How | Speed |", "Pose estimation"} {
		if !strings.Contains(cleaned, want) {
			t.Fatalf("cleaned README missing %q:\n%s", want, cleaned)
		}
	}
	if strings.Contains(cleaned, "img.shields.io") {
		t.Fatalf("badge leaked into cleaned README:\n%s", cleaned)
	}
}

func TestImageRefsResolveRelativeRawAsset(t *testing.T) {
	refs := ImageRefs("![screen](assets/v2-screen.png)", "https://github.com/ruvnet/RuView/blob/main/README.md")
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want one", refs)
	}
	want := "https://raw.githubusercontent.com/ruvnet/RuView/main/assets/v2-screen.png"
	if refs[0].URL != want {
		t.Fatalf("resolved image URL = %q, want %q", refs[0].URL, want)
	}
}

func TestResolveImageURLUsesRawGitHubRefsHeadURL(t *testing.T) {
	got, ok := ResolveImageURL("assets/v2-screen.png", "https://raw.githubusercontent.com/ruvnet/RuView/refs/heads/main/README.md")
	if !ok {
		t.Fatal("expected raw refs/heads README image to resolve")
	}
	want := "https://raw.githubusercontent.com/ruvnet/RuView/main/assets/v2-screen.png"
	if got != want {
		t.Fatalf("resolved image URL = %q, want %q", got, want)
	}
}

func TestImageRefsSkipBadgesAndSupportHTMLImages(t *testing.T) {
	readmeURL := "https://github.com/owner/repo/blob/main/docs/README.md"
	markdown := strings.Join([]string{
		"[![Build](https://img.shields.io/badge/build-passing-green.svg)](https://github.com/owner/repo/actions)",
		`<p><img src="../assets/screen.png" alt="Screen"></p>`,
	}, "\n")

	images := ImageRefs(markdown, readmeURL)
	if len(images) != 1 {
		t.Fatalf("images = %+v, want one non-badge image", images)
	}
	if images[0].Alt != "Screen" || images[0].Src != "../assets/screen.png" {
		t.Fatalf("image ref = %+v", images[0])
	}
	wantURL := "https://raw.githubusercontent.com/owner/repo/main/assets/screen.png"
	if images[0].URL != wantURL {
		t.Fatalf("image URL = %q, want %q", images[0].URL, wantURL)
	}
}

func TestSectionFindsGitHubReadme(t *testing.T) {
	detail := domain.ItemDetail{
		Sections: []domain.DetailSection{
			{Title: "Other", Body: "ignore"},
			{Title: "GitHub README", Body: "# README", Source: domain.SourceGitHub},
		},
	}
	section, ok := Section(detail)
	if !ok {
		t.Fatal("expected README section")
	}
	if section.Body != "# README" {
		t.Fatalf("section = %+v", section)
	}
}
