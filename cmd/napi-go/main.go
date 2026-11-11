// Command napi-go creates, builds and packages Node.js addons written in Go with napi-go (the
// equivalent of the napi CLI of napi-rs).
//
//	napi-go new <dir>          create an addon project
//	napi-go gen                generate the registrations, index.js and index.d.ts
//	napi-go build              build the .node file for this machine or for other targets
//	napi-go artifacts          put built binaries into the npm platform packages
//	napi-go prepublish         sync versions and optionalDependencies, optionally publish them
//
// Inside a project it is run as a Go tool, with the version required by go.mod:
//
//	go tool napi-go build -release
//
// See README.md for the configuration in package.json, the targets and the publishing workflow.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// command is a subcommand of napi-go.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) error
}

// commands are the subcommands, in the order of the usage message.
var commands = []command{
	{"new", "create an addon project", runNew},
	{"gen", "generate the registrations, index.js and index.d.ts from //napi: annotations", runGen},
	{"build", "build the .node file for this machine or for other targets", runBuild},
	{"artifacts", "put built binaries into the npm platform packages", runArtifacts},
	{"prepublish", "sync versions and optionalDependencies of the platform packages, optionally publish them", runPrepublish},
	{"version", "print the version of napi-go", runVersion},
}

// Exit codes.
const (
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line args and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		usage(stderr)
		if len(args) == 0 {
			return exitUsage
		}
		return 0
	}
	for _, c := range commands {
		if c.name != args[0] {
			continue
		}
		err := c.run(args[1:], stdout, stderr)
		switch {
		case err == nil:
			return 0
		case errors.Is(err, flag.ErrHelp):
			return 0
		case errors.Is(err, errUsage):
			return exitUsage
		}
		fmt.Fprintf(stderr, "napi-go %s: %v\n", c.name, err)
		return exitFailure
	}
	fmt.Fprintf(stderr, "napi-go: unknown command %q\n\n", args[0])
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: napi-go <command> [flags]")
	fmt.Fprintln(w)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `"napi-go <command> -h" describes the flags of a command.`)
}

// errUsage reports a command line error whose message the flag set has already printed.
var errUsage = errors.New("usage")

// newFlagSet returns the flag set of a command: errors and -h print to stderr and are returned.
func newFlagSet(stderr io.Writer, name, args string) *flag.FlagSet {
	fs := flag.NewFlagSet("napi-go "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: napi-go %s [flags] %s\n", name, args)
		fs.PrintDefaults()
	}
	return fs
}

// parseFlags parses args, turning a flag error into errUsage.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	return nil
}

// modulePath is the module of napi-go.
const modulePath = "github.com/napi-bindings/napi-go"

// develVersion is the version of a napi-go built from a working tree.
const develVersion = "(devel)"

// version returns the version of the napi-go module this command was built from: the main module
// when it is installed with go install, a dependency when it runs as a tool of an addon.
func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return develVersion
	}
	if bi.Main.Path == modulePath {
		return bi.Main.Version
	}
	for _, d := range bi.Deps {
		if d.Path == modulePath {
			if d.Replace != nil {
				return develVersion
			}
			return d.Version
		}
	}
	return develVersion
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "version", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout, "napi-go", version())
	return err
}
