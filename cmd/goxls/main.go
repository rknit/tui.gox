// Command goxls is a language server for .gox files. It wraps gopls, so
// every Go feature works inside .gox files, and adds element-aware
// completion. Plain .go files are served by gopls unchanged, so goxls can
// replace gopls for a whole project.
//
// Usage:
//
//	goxls [-gopls path] [-logfile path] [-- gopls args...]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rknit/tui.gox/lsp"
)

func main() {
	gopls := flag.String("gopls", "gopls", "gopls command")
	logfile := flag.String("logfile", "", "write debug logs to this file")
	flag.Bool("stdio", true, "use stdio (the only transport; accepted for client compatibility)")
	flag.Parse()

	var logw io.Writer
	if *logfile != "" {
		f, err := os.OpenFile(*logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		logw = f
	}
	if err := lsp.Run(os.Stdin, os.Stdout, lsp.Options{Gopls: *gopls, GoplsArgs: flag.Args(), Log: logw}); err != nil {
		fmt.Fprintln(os.Stderr, "goxls:", err)
		os.Exit(1)
	}
}
