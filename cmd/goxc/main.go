// Command goxc compiles .gox files (Go with XML elements) into Go.
//
// Usage:
//
//	goxc [packages]          generate foo_gox.go next to every foo.gox
//	goxc run [package] [args] generate, then go run the package
//
// Packages are directories; a trailing /... recurses. Default is ".".
// After writing files goxc runs `go mod tidy` in the enclosing module so new
// imports (including the gox runtime) are added to go.mod; disable with
// -tidy=false.
// Typical use is a go:generate directive:
//
//	//go:generate go run github.com/rknit/tui.gox/cmd/goxc .
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rknit/tui.gox/transpile"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: goxc [-check] [packages]\n       goxc run [package] [args...]")
		flag.PrintDefaults()
	}
	check := flag.Bool("check", false, "report files whose generated output is stale, without writing")
	verbose := flag.Bool("v", false, "print generated files")
	tidy := flag.Bool("tidy", true, "run go mod tidy after generating so new imports are added to go.mod")
	flag.Parse()
	args := flag.Args()

	if len(args) > 0 && args[0] == "run" {
		pkg := "."
		rest := args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			pkg, rest = rest[0], rest[1:]
		}
		if err := generate([]string{pkg}, false, *verbose, *tidy); err != nil {
			fail(err)
		}
		cmd := exec.Command("go", append([]string{"run", pkg}, rest...)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				os.Exit(ee.ExitCode())
			}
			fail(err)
		}
		return
	}

	if len(args) == 0 {
		args = []string{"."}
	}
	if err := generate(args, *check, *verbose, *tidy); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func generate(patterns []string, check, verbose, tidy bool) error {
	dirs, err := expand(patterns)
	if err != nil {
		return err
	}
	var errs []error
	stale := false
	modules := map[string]bool{} // module root -> needs tidy
	for _, d := range dirs {
		s, wrote, err := generateDir(d, check, verbose)
		errs = append(errs, err)
		stale = stale || s
		if root := moduleRoot(d); root != "" {
			modules[root] = modules[root] || wrote
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if tidy && !check {
		for root, wrote := range modules {
			if !wrote && requiresRuntime(root) {
				continue
			}
			if verbose {
				fmt.Printf("go mod tidy (%s)\n", root)
			}
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("goxc: go mod tidy in %s failed: %v\n%s", root, err, out)
			}
		}
	}
	if check && stale {
		return errors.New("goxc: generated files are stale; run goxc")
	}
	return nil
}

// expand resolves directory patterns into directories containing .gox files.
func expand(patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, p := range patterns {
		if strings.HasSuffix(p, ".gox") {
			add(filepath.Dir(p))
			continue
		}
		if root, ok := strings.CutSuffix(p, "/..."); ok {
			if root == "" {
				root = "."
			}
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") || d.Name() == "testdata" || d.Name() == "vendor") {
					return filepath.SkipDir
				}
				if !d.IsDir() && strings.HasSuffix(path, ".gox") {
					add(filepath.Dir(path))
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		add(p)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// moduleRoot returns the directory of the go.mod governing dir, or "".
func moduleRoot(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// requiresRuntime reports whether the module's go.mod already provides the
// gox runtime (it requires it, or is the runtime module itself).
func requiresRuntime(root string) bool {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	return err == nil && bytes.Contains(b, []byte(runtimeModule))
}

const runtimeModule = "github.com/rknit/tui.gox"

func generateDir(dir string, check, verbose bool) (stale, wrote bool, err error) {
	pkg, err := transpile.LoadPackage(dir, nil)
	if err != nil {
		return false, false, err
	}
	var errs []error
	for _, path := range pkg.Paths {
		out, _, err := pkg.Transpile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		target := transpile.OutputName(path)
		old, _ := os.ReadFile(target)
		if bytes.Equal(old, out) {
			continue
		}
		if check {
			fmt.Fprintf(os.Stderr, "stale: %s\n", target)
			stale = true
			continue
		}
		if err := os.WriteFile(target, out, 0o644); err != nil {
			errs = append(errs, err)
			continue
		}
		wrote = true
		if verbose {
			fmt.Println(target)
		}
	}
	return stale, wrote, errors.Join(errs...)
}
