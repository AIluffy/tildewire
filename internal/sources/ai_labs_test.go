package sources

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

func TestAILabsOpenAIRSSNormalizesNewsItems(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceAILabs,
		FetchedAt: fetchedAt,
		Body:      []byte(aiLabsOpenAIRSSFixture),
	}
	items, err := NewAILabsAdapter().Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceAILabs,
		View:   "openai",
		Limit:  10,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if len(item.Sources) != 1 || item.Sources[0].Source != domain.SourceAILabs {
		t.Fatalf("source context missing AI Labs: %+v", item.Sources)
	}
	if item.Title != "Introducing GPT-5.1" || item.Organization != "OpenAI" || item.ItemType != "news" {
		t.Fatalf("item fields not normalized: %+v", item)
	}
	if item.CanonicalURL != "https://openai.com/news/gpt-5-1" || item.CanonicalKey != "url:https://openai.com/news/gpt-5-1" {
		t.Fatalf("canonical fields mismatch: %+v", item)
	}
	if item.PublishedAt == nil || !item.PublishedAt.Equal(time.Date(2026, 5, 11, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("published at = %+v", item.PublishedAt)
	}
	if item.Sources[0].SourceView != "openai" || item.Sources[0].SourceRank != 1 || item.Sources[0].SourceIDRaw == "" {
		t.Fatalf("source context mismatch: %+v", item.Sources[0])
	}
	if !hasAILabsString(item.Tags, "openai") || !strings.Contains(item.Summary, "new model") {
		t.Fatalf("summary/tags mismatch: summary=%q tags=%+v", item.Summary, item.Tags)
	}
}

func TestAILabsNormalizeSortsByPublishedDateBeforeLimit(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceAILabs,
		FetchedAt: fetchedAt,
		Body: []byte(`
<rss version="2.0">
  <channel>
    <item>
      <title>Older update</title>
      <link>https://openai.com/news/older-update/</link>
      <pubDate>Mon, 04 May 2026 17:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Newer update</title>
      <link>https://openai.com/news/newer-update/</link>
      <pubDate>Mon, 18 May 2026 17:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`),
	}
	items, err := NewAILabsAdapter().Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceAILabs,
		View:   "openai",
		Limit:  1,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Newer update" || items[0].Sources[0].SourceRank != 1 {
		t.Fatalf("items = %+v, want newest item with rank 1", items)
	}
}

func TestAILabsHTMLNormalizesProviderNewsItems(t *testing.T) {
	tests := []struct {
		name    string
		view    string
		org     string
		wantURL string
		body    string
	}{
		{
			name:    "anthropic",
			view:    "anthropic",
			org:     "Anthropic",
			wantURL: "https://www.anthropic.com/news/claude-model-update",
			body:    aiLabsAnthropicHTMLFixture,
		},
		{
			name:    "deepmind",
			view:    "deepmind",
			org:     "Google DeepMind",
			wantURL: "https://deepmind.google/blog/genie-3-world-model",
			body:    aiLabsDeepMindHTMLFixture,
		},
		{
			name:    "meta",
			view:    "meta",
			org:     "Meta AI",
			wantURL: "https://ai.meta.com/blog/llama-open-model-update",
			body:    aiLabsMetaHTMLFixture,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := &domain.FetchResult{
				Source:    domain.SourceAILabs,
				FetchedAt: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
				Body:      []byte(tt.body),
			}
			items, err := NewAILabsAdapter().Normalize(context.Background(), domain.FetchScope{
				Source: domain.SourceAILabs,
				View:   tt.view,
				Limit:  10,
			}, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("items = %d, want 1", len(items))
			}
			item := items[0]
			if item.Organization != tt.org || item.CanonicalURL != tt.wantURL {
				t.Fatalf("item mismatch: %+v", item)
			}
			if item.Sources[0].SourceView != tt.view || !hasAILabsString(item.Tags, tt.view) {
				t.Fatalf("source view/tags mismatch: source=%+v tags=%+v", item.Sources[0], item.Tags)
			}
			if item.PublishedAt == nil {
				t.Fatalf("published date missing: %+v", item)
			}
		})
	}
}

func hasAILabsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

const aiLabsOpenAIRSSFixture = `
<rss version="2.0">
  <channel>
    <item>
      <title>Introducing GPT-5.1</title>
      <link>https://openai.com/news/gpt-5-1/</link>
      <description><![CDATA[<p>A new model update for developers and ChatGPT users.</p>]]></description>
      <pubDate>Mon, 11 May 2026 17:00:00 GMT</pubDate>
      <category>Company</category>
    </item>
  </channel>
</rss>`

const aiLabsAnthropicHTMLFixture = `
<html>
  <body>
    <main>
      <article>
        <a href="/news/claude-model-update"><h2>Claude model update</h2></a>
        <time datetime="2026-05-12">May 12, 2026</time>
        <p>We are sharing new progress on Claude.</p>
      </article>
    </main>
  </body>
</html>`

const aiLabsDeepMindHTMLFixture = `
<html>
  <body>
    <main>
      <article>
        <a href="/blog/genie-3-world-model/"><h2>Genie 3 world model</h2></a>
        <time datetime="2026-05-13">13 May 2026</time>
        <p>A new AI system for interactive world generation.</p>
      </article>
    </main>
  </body>
</html>`

const aiLabsMetaHTMLFixture = `
<html>
  <body>
    <main>
      <article>
        <a href="/blog/llama-open-model-update/"><h2>Llama open model update</h2></a>
        <time datetime="2026-05-14">May 14, 2026</time>
        <p>Updates on open models from Meta AI.</p>
      </article>
    </main>
  </body>
</html>`
