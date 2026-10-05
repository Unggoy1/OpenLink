// hi-host-preflight verifies an on-disk executable without running it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"halocommunity/internal/hostctl"
)

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("hi-host-preflight", flag.ContinueOnError)
	flags.SetOutput(stderr)
	exe := flags.String("exe", "", "path to the on-disk HaloInfinite.exe")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: hi-host-preflight -exe <path>")
		fmt.Fprintln(stderr, "Performs read-only file verification. No DLL is loaded and no process is controlled.")
		fmt.Fprintln(stderr, "Exit codes: 0 verified file only; 3 unsupported file; 2 usage, I/O or malformed PE error.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *exe == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Exactly one -exe path is required; positional commands are unsupported.")
		flags.Usage()
		return 2
	}
	report, err := hostctl.InspectFile(*exe)
	if err != nil {
		fmt.Fprintf(stderr, "Preflight failed: %v\n", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "Write report: %v\n", err)
		return 2
	}
	if !report.SupportedBuild {
		return 3
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
