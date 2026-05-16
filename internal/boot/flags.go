package boot

import (
	"flag"
	"fmt"
	"io"
)

// Options captures minimal command-line flags.
type Options struct {
	ConfigPath  string
	Debug       bool
	DebugSet    bool
	ShowHelp    bool
	ShowVersion bool
}

// ParseArgs parses tildewire's v0.1 command shape.
func ParseArgs(args []string, output io.Writer) (Options, error) {
	return ParseArgsFor("tildewire", args, output)
}

// ParseArgsFor parses tildewire's command shape using programName in help text.
func ParseArgsFor(programName string, args []string, output io.Writer) (Options, error) {
	if programName == "" {
		programName = "tildewire"
	}
	var options Options
	flags := flag.NewFlagSet(programName, flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&options.ConfigPath, "config", "", "path to config.toml")
	flags.BoolVar(&options.Debug, "debug", false, "write debug logs")
	flags.BoolVar(&options.ShowVersion, "version", false, "print version")
	flags.BoolVar(&options.ShowHelp, "help", false, "print help")
	flags.Usage = func() {
		fmt.Fprintf(output, "%s - terminal daily technical signal radar\n", programName)
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Usage:")
		fmt.Fprintf(output, "  %s [--config path] [--debug] [--version] [--help]\n", programName)
		fmt.Fprintln(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return Options{}, err
	}
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "debug" {
			options.DebugSet = true
		}
	})
	if options.ShowHelp {
		flags.Usage()
	}
	return options, nil
}
