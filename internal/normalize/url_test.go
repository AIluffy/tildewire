package normalize

import "testing"

func TestCanonicalURLRemovesTrackingAndSortsQuery(t *testing.T) {
	got := CanonicalURL("HTTPS://Example.COM:443//a/../b/?utm_source=hn&z=2&a=1#frag")
	want := "https://example.com/b?a=1&z=2"
	if got != want {
		t.Fatalf("CanonicalURL() = %q, want %q", got, want)
	}
}

func TestStableID(t *testing.T) {
	first := StableID("hackernews:1")
	second := StableID("hackernews:1")
	if first == "" || first != second {
		t.Fatalf("StableID not deterministic: %q %q", first, second)
	}
}
