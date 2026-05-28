package markdown

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCacheMarkdownImageSendsConfiguredUserAgent(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()

	if _, err := cacheMarkdownImage(context.Background(), server.URL+"/image.png", t.TempDir(), "tildewire/0.4.0"); err != nil {
		t.Fatal(err)
	}
	if got != "tildewire/0.4.0" {
		t.Fatalf("user agent = %q, want tildewire/0.4.0", got)
	}
}
