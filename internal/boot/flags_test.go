package boot

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgsForUsesProgramNameInHelp(t *testing.T) {
	var output bytes.Buffer

	options, err := ParseArgsFor("tildewire", []string{"--help"}, &output)
	if err != nil {
		t.Fatalf("ParseArgsFor returned error: %v", err)
	}
	if !options.ShowHelp {
		t.Fatal("ShowHelp = false, want true")
	}

	help := output.String()
	for _, want := range []string{
		"tildewire - terminal daily technical signal radar",
		"  tildewire [--config path] [--debug] [--version] [--help]",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
}

func TestParseArgsDefaultsToCanonicalProgramName(t *testing.T) {
	var output bytes.Buffer

	_, err := ParseArgs([]string{"--help"}, &output)
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	help := output.String()
	if !strings.Contains(help, "  tildewire [--config path] [--debug] [--version] [--help]") {
		t.Fatalf("help missing canonical usage:\n%s", help)
	}
}
