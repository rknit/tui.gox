package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rknit/tui.gox/transpile"
)

// runFmt implements `goxc fmt`, a gofmt for .gox files.
func runFmt(args []string) error {
	fset := flag.NewFlagSet("fmt", flag.ExitOnError)
	write := fset.Bool("w", false, "write result to the source file instead of stdout")
	list := fset.Bool("l", false, "list files whose formatting differs")
	diff := fset.Bool("d", false, "display diffs instead of rewriting files")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: goxc fmt [-w] [-l] [-d] [path ...]\n\nWith no paths, formats stdin to stdout. Directories are searched recursively.")
		fset.PrintDefaults()
	}
	_ = fset.Parse(args)

	if fset.NArg() == 0 {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		out, err := transpile.Format(src)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	}

	var files []string
	for _, p := range fset.Args() {
		p = strings.TrimSuffix(p, "/...")
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != p && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(path, ".gox") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	var errs []error
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out, err := transpile.Format(src)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		changed := !bytes.Equal(src, out)
		if *list && changed {
			fmt.Println(path)
		}
		if *diff && changed {
			d, err := unifiedDiff(path, src, out)
			if err != nil {
				errs = append(errs, err)
			}
			os.Stdout.Write(d)
		}
		if *write && changed {
			if err := os.WriteFile(path, out, 0o644); err != nil {
				errs = append(errs, err)
			}
		}
		if !*list && !*diff && !*write {
			os.Stdout.Write(out)
		}
	}
	return errors.Join(errs...)
}

func unifiedDiff(path string, a, b []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "goxfmt")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	fa, fb := filepath.Join(dir, "orig"), filepath.Join(dir, "formatted")
	if err := os.WriteFile(fa, a, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(fb, b, 0o644); err != nil {
		return nil, err
	}
	out, err := exec.Command("diff", "-u", "--label", path+".orig", "--label", path, fa, fb).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		err = nil
	}
	return out, err
}
