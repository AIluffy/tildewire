package normalize

import "testing"

func TestArxivIDFromURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "abstract URL",
			raw:  "https://arxiv.org/abs/2605.12345v2?utm_source=hn",
			want: "2605.12345v2",
		},
		{
			name: "pdf URL",
			raw:  "https://arxiv.org/pdf/2605.12345.pdf",
			want: "2605.12345",
		},
		{
			name: "non arxiv URL",
			raw:  "https://example.com/2605.12345",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ArxivIDFromURL(tt.raw)
			if got != tt.want || ok != (tt.want != "") {
				t.Fatalf("ArxivIDFromURL(%q) = %q/%v, want %q/%v", tt.raw, got, ok, tt.want, tt.want != "")
			}
		})
	}
}
