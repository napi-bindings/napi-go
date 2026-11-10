// Command napi-go-gen generates the Layer 2 registrations and the TypeScript declarations of a
// napi-go addon from annotations in its Go source (the equivalent of #[napi] and of the type
// generation of napi-rs).
//
// Annotate the package and run the generator in its directory, usually with go generate:
//
//	//go:generate go run github.com/napi-bindings/napi-go/cmd/napi-go-gen
//
//	// Add returns a + b.
//	//napi:export
//	func Add(a, b float64) float64 { return a + b }
//
// It writes zz_napi.go (an init function calling napi.Export for every annotated declaration),
// index.d.ts and index.js. Types that cannot be converted are reported with their position,
// before the addon is built. See README.md for the annotations and the TypeScript mapping.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Default output file names, relative to the package directory.
const (
	defaultGoFile  = "zz_napi.go"
	defaultDTSFile = "index.d.ts"
	defaultJSFile  = "index.js"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.goFile, "out", defaultGoFile, "generated Go file, relative to the package directory")
	flag.StringVar(&cfg.dtsFile, "dts", defaultDTSFile, "generated TypeScript declarations (empty: none)")
	flag.StringVar(&cfg.jsFile, "js", defaultJSFile, "generated JS entry point (empty: none)")
	flag.StringVar(&cfg.name, "name", "", "base name of the .node file loaded by the JS entry point (default: the directory name)")
	flag.StringVar(&cfg.tags, "tags", "", "build tags, as for go build")
	check := flag.Bool("check", false, "do not write: fail if the generated files are out of date")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: napi-go-gen [flags] [package directory]\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	cfg.dir = "."
	switch flag.NArg() {
	case 0:
	case 1:
		cfg.dir = flag.Arg(0)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err := run(cfg, *check); err != nil {
		fmt.Fprintln(os.Stderr, "napi-go-gen:", err)
		os.Exit(1)
	}
}

// config is one run of the generator.
type config struct {
	dir     string // package directory
	goFile  string
	dtsFile string
	jsFile  string
	name    string
	tags    string
}

// output is a generated file.
type output struct {
	path    string
	content []byte
}

// run generates the files of cfg and writes them, or with check compares them with the files on
// disk.
func run(cfg config, check bool) error {
	outs, err := generate(cfg)
	if err != nil {
		return err
	}
	for _, o := range outs {
		if check {
			cur, err := os.ReadFile(o.path)
			if err != nil {
				return fmt.Errorf("%s: %w (run napi-go-gen)", o.path, err)
			}
			if !bytes.Equal(cur, o.content) {
				return fmt.Errorf("%s is out of date: run napi-go-gen", o.path)
			}
			continue
		}
		if err := os.WriteFile(o.path, o.content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// generate loads the package of cfg and returns its generated files.
func generate(cfg config) ([]output, error) {
	dir, err := filepath.Abs(cfg.dir)
	if err != nil {
		return nil, err
	}
	if cfg.name == "" {
		cfg.name = filepath.Base(dir)
	}
	p, err := load(dir, cfg.tags, cfg.goFile)
	if err != nil {
		return nil, err
	}
	m, err := analyze(p)
	if err != nil {
		return nil, err
	}
	goSrc, err := m.goSource()
	if err != nil {
		return nil, err
	}
	outs := []output{{filepath.Join(dir, cfg.goFile), goSrc}}
	if cfg.dtsFile != "" {
		outs = append(outs, output{filepath.Join(dir, cfg.dtsFile), m.dts()})
	}
	if cfg.jsFile != "" {
		outs = append(outs, output{filepath.Join(dir, cfg.jsFile), jsEntry(cfg.name)})
	}
	return outs, nil
}
