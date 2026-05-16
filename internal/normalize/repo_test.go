package normalize

import "testing"

func TestNormalizeRepo(t *testing.T) {
	tests := map[string]string{
		"Charmbracelet/BubbleTea":        "charmbracelet/bubbletea",
		" /Owner/Repo/ ":                 "owner/repo",
		"https://github.com/Owner/Repo":  "",
		"owner":                          "",
		"https://github.com/Owner/Repo/": "",
	}
	for input, want := range tests {
		if got := NormalizeRepo(input); got != want {
			t.Fatalf("NormalizeRepo(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRepoKeyAndPath(t *testing.T) {
	if got := RepoKey("Owner/Repo"); got != "repo:owner/repo" {
		t.Fatalf("RepoKey = %q", got)
	}
	repo, ok := RepoFromPath("https://github.com/Charmbracelet/BubbleTea")
	if !ok || repo != "charmbracelet/bubbletea" {
		t.Fatalf("RepoFromPath url = %q, %v", repo, ok)
	}
	repo, ok = RepoFromPath("/Owner/Repo/stargazers")
	if !ok || repo != "owner/repo" {
		t.Fatalf("RepoFromPath path = %q, %v", repo, ok)
	}
}
