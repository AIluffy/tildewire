package launcher

import "testing"

func TestDisplayNameUsesTwWhenInvokedThroughShortName(t *testing.T) {
	tests := []struct {
		name string
		argv string
		want string
	}{
		{name: "canonical", argv: "/usr/local/bin/tildewire", want: "tildewire"},
		{name: "short", argv: "/usr/local/bin/tw", want: "tw"},
		{name: "empty", argv: "", want: "tildewire"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DisplayName(tt.argv); got != tt.want {
				t.Fatalf("DisplayName(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}
