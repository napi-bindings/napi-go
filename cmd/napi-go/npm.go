package main

// npm.go — napi-go artifacts and napi-go prepublish: the npm platform packages. As with napi-rs,
// each target has a package <name>-<target> holding its .node file, with "os", "cpu" and "libc"
// so that npm installs only the one of the machine; the main package lists them all in
// optionalDependencies, and its index.js loads the installed one.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runArtifacts(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "artifacts", "")
	dir := fs.String("C", ".", "project directory")
	from := fs.String("d", "artifacts", "directory searched (recursively) for the built .node files, relative to the project")
	targetSpec := fs.String("target", "all", "comma-separated targets, or all for the targets of package.json")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	p, err := loadProject(*dir)
	if err != nil {
		return err
	}
	if err := p.requirePackageJSON("artifacts"); err != nil {
		return err
	}
	targets, err := resolveTargets(*targetSpec, p.cfg.Targets)
	if err != nil {
		return err
	}
	found, err := findBinaries(p, p.path(*from), targets)
	if err != nil {
		return err
	}
	var missing []string
	for _, t := range targets {
		src, ok := found[t.name]
		if !ok {
			missing = append(missing, t.binaryFile(p.cfg.BinaryName))
			continue
		}
		// An artifact renamed or downloaded into the wrong place would otherwise be published for
		// the wrong platform.
		if err := checkBinary(src, t); err != nil {
			return err
		}
		pkgDir, err := syncPlatformPackage(p, t)
		if err != nil {
			return err
		}
		dst := filepath.Join(pkgDir, t.binaryFile(p.cfg.BinaryName))
		if err := copyFile(src, dst); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "%s -> %s\n", relTo(p.dir, src), relTo(p.dir, dst)); err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("not found in %s: %s", *from, strings.Join(missing, ", "))
	}
	return nil
}

// findBinaries searches root for the .node files of targets. The platform packages and
// node_modules are skipped: they hold copies.
func findBinaries(p *project, root string, targets []target) (map[string]string, error) {
	want := map[string]string{} // file name -> target
	for _, t := range targets {
		want[t.binaryFile(p.cfg.BinaryName)] = t.name
	}
	skip := map[string]bool{p.path(p.cfg.NpmDir): true}
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[path] || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name, ok := want[d.Name()]
		if !ok || !d.Type().IsRegular() {
			return nil
		}
		if prev, dup := found[name]; dup {
			return fmt.Errorf("%s found twice: %s and %s", d.Name(), prev, path)
		}
		found[name] = path
		return nil
	})
	return found, err
}

// copyFile copies a regular file.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// relTo returns path relative to base for messages, or path itself.
func relTo(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

// copiedFields are copied from the main package.json into the platform packages.
var copiedFields = []string{"license", "author", "repository", "homepage", "bugs", "engines", "publishConfig"}

// syncPlatformPackage creates or updates npm/<target>/package.json (and a README) and returns the
// directory. Fields it does not manage are kept.
func syncPlatformPackage(p *project, t target) (string, error) {
	dir := filepath.Join(p.path(p.cfg.NpmDir), t.name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, packageJSONFile)
	pkg, err := readJSONObject(path)
	if errors.Is(err, fs.ErrNotExist) {
		pkg, err = newJSONObject(), nil
	}
	if err != nil {
		return "", err
	}
	file := t.binaryFile(p.cfg.BinaryName)
	fields := []struct {
		key string
		val any
	}{
		{"name", t.packageName(p.name)},
		{"version", p.version},
		{"description", fmt.Sprintf("The %s binary of %s", t.name, p.name)},
		{"main", file},
		{"files", []string{file}},
		{"os", []string{t.platform}},
		{"cpu", []string{t.arch}},
	}
	for _, f := range fields {
		if f.key == "description" && pkg.has(f.key) {
			continue
		}
		if err := pkg.set(f.key, f.val); err != nil {
			return "", err
		}
	}
	if t.libc != "" {
		if err := pkg.set("libc", []string{t.libc}); err != nil {
			return "", err
		}
	} else {
		pkg.remove("libc")
	}
	for _, k := range copiedFields {
		if raw, ok := p.pkg.values[k]; ok {
			pkg.setRaw(k, raw)
		}
	}
	if err := writeJSONObject(path, pkg); err != nil {
		return "", err
	}
	readme := filepath.Join(dir, "README.md")
	if ok, err := fileExists(readme); err != nil {
		return "", err
	} else if !ok {
		text := fmt.Sprintf("# %s\n\nThe **%s** binary of [`%s`](https://www.npmjs.com/package/%s), installed by npm as an optional dependency on matching machines.\n", t.packageName(p.name), t.name, p.name, p.name)
		if err := os.WriteFile(readme, []byte(text), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func runPrepublish(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "prepublish", "")
	dir := fs.String("C", ".", "project directory")
	publish := fs.Bool("publish", false, "run npm publish in every platform package not yet published at this version")
	tag := fs.String("tag", "", "npm dist-tag for -publish")
	dryRun := fs.Bool("dry-run", false, "with -publish: npm publish --dry-run")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	p, err := loadProject(*dir)
	if err != nil {
		return err
	}
	if err := p.requirePackageJSON("prepublish"); err != nil {
		return err
	}
	targets, err := resolveTargets("all", p.cfg.Targets)
	if err != nil {
		return err
	}
	var dirs []string
	for _, t := range targets {
		d, err := syncPlatformPackage(p, t)
		if err != nil {
			return err
		}
		bin := filepath.Join(d, t.binaryFile(p.cfg.BinaryName))
		if ok, err := fileExists(bin); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%s is missing: run napi-go artifacts", relTo(p.dir, bin))
		}
		dirs = append(dirs, d)
	}
	if err := syncOptionalDependencies(p, targets); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "%d platform packages at version %s, listed in optionalDependencies\n", len(targets), p.version); err != nil {
		return err
	}
	if !*publish {
		return nil
	}
	for i, t := range targets {
		if err := publishPlatformPackage(p, t, dirs[i], *tag, *dryRun, stdout); err != nil {
			return fmt.Errorf("%s: %w", t.packageName(p.name), err)
		}
	}
	return nil
}

// syncOptionalDependencies lists the platform packages of targets in the optionalDependencies of
// package.json, at the version of the package, and removes the ones of other targets.
func syncOptionalDependencies(p *project, targets []target) error {
	deps, err := p.pkg.object("optionalDependencies")
	if err != nil {
		return err
	}
	for _, t := range allTargets {
		deps.remove(t.packageName(p.name))
	}
	for _, t := range targets {
		if err := deps.set(t.packageName(p.name), p.version); err != nil {
			return err
		}
	}
	raw, err := deps.MarshalJSON()
	if err != nil {
		return err
	}
	p.pkg.setRaw("optionalDependencies", raw)
	return p.writePackageJSON()
}

// runNpm runs npm in dir and returns its standard output. It is a variable so that tests check
// the npm commands without a registry.
var runNpm = func(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("npm", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("npm %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// publishPlatformPackage publishes the platform package of t unless this version is already on
// the registry (a rerun of a failed release publishes the rest).
func publishPlatformPackage(p *project, t target, dir, tag string, dryRun bool, stdout io.Writer) error {
	spec := t.packageName(p.name) + "@" + p.version
	if out, err := runNpm(dir, "view", spec, "version"); err == nil && strings.TrimSpace(string(out)) == p.version {
		_, err := fmt.Fprintf(stdout, "%s already published\n", spec)
		return err
	}
	args := []string{"publish"}
	if tag != "" {
		args = append(args, "--tag", tag)
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	// A scoped package is private by default: publish it as public unless publishConfig decides.
	if strings.HasPrefix(p.name, "@") && !p.pkg.has("publishConfig") {
		args = append(args, "--access", "public")
	}
	if _, err := runNpm(dir, args...); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "published %s\n", spec)
	return err
}
