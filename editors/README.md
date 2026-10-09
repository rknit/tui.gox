# Editor support

`goxls` is a language server for `.gox` files. It runs [gopls](https://pkg.go.dev/golang.org/x/tools/gopls)
behind the scenes:

- Each open `.gox` buffer is transpiled in memory.
- gopls sees the generated code as an unsaved version of `foo_gox.go`.
- Positions are translated both ways through the transpiler's source maps.

`goxls` works with any LSP client over stdio.

```sh
go install golang.org/x/tools/gopls@latest
go install github.com/rknit/tui.gox/cmd/goxls@main
go install github.com/rknit/tui.gox/cmd/goxc@main   # for `goxc fmt` (optional)
```

## Features

- **Diagnostics:**
  - gopls type errors appear at their `.gox` positions.
  - goxc errors, such as mismatched tags or unknown elements, appear as you type.
- **Navigation:** hover, go-to-definition, references and rename work in Go code, `{expressions}`,
  tag names (`<Card` resolves to the `Card` component) and attribute names (`title=` resolves to
  `CardProps.Title`). Plain `.go` files that refer to `.gox` code resolve to the `.gox` source.
- **Completion:**
  - Go completion inside expressions.
  - Tag names after `<`: built-ins plus the package's components.
  - Attribute names inside an opening tag, taken from the props struct, skipping those already
    set.
- **Generate on save:** `foo_gox.go` is rewritten when `foo.gox` is saved, so `go build` and
  `go run` work without running `goxc`. Set `generateOnSave: false` to turn this off.

- **Formatting:** `textDocument/formatting` formats the whole document, the same way as
  `goxc fmt`. If the document has errors, the request fails with the error message.

**Not supported for `.gox` files:** range formatting and semantic tokens. Syntax highlighting comes
from the [tree-sitter grammar](tree-sitter-gox).

### Options

| Option | How to set it |
|---|---|
| `-gopls <path>` | Flag. Path to the gopls binary. |
| `-logfile <path>` | Flag. Write a debug log to this file. |
| `-- <args>` | Flag. Extra arguments for gopls. |
| `generateOnSave` | Initialization option `{ "goxls": { "generateOnSave": false } }` |

Any other initialization options and `settings` are passed to gopls.

## Neovim

The `editors/nvim` directory is a Neovim plugin. It provides:

- `.gox` filetype detection.
- Tree-sitter highlighting.
- A fallback regex syntax.
- An ftplugin.
- The LSP setup.

With [lazy.nvim](https://github.com/folke/lazy.nvim) or LazyVim:

```lua
{
  "rknit/tui.gox",
  -- Required when lazy-loading is the default: Neovim only knows the "gox"
  -- filetype after this plugin loads, so `ft = "gox"` can never trigger it.
  -- lazy = false also works.
  event = { "BufReadPre *.gox", "BufNewFile *.gox" },
  config = function(plugin)
    vim.opt.rtp:append(plugin.dir .. "/editors/nvim")
    require("goxls").setup({
      -- cmd = { "goxls", "-gopls", "/path/to/gopls" },
      -- generate_on_save = true,
      -- treesitter = true,
      -- settings = { gopls = { gofumpt = true, staticcheck = true } },
      -- capabilities = require("blink.cmp").get_lsp_capabilities(),
    })
  end,
}
```

Formatting works through `vim.lsp.buf.format()`. With conform.nvim (LazyVim's formatter), either
let it fall back to the LSP, or run `goxc fmt` directly:

```lua
{
  "stevearc/conform.nvim",
  opts = {
    formatters_by_ft = { gox = { "goxfmt" } }, -- or { gox = {} } with lsp_format = "fallback"
    formatters = { goxfmt = { command = "goxc", args = { "fmt" }, stdin = true } },
  },
}
```

If you set gopls options in your gopls config, add them under `settings.gopls` here as well. goxls
runs its own gopls instance, and that instance reads its settings from the goxls client.

To set it up manually, add the directory to `runtimepath`:

```lua
vim.opt.rtp:append("/path/to/tui.gox/editors/nvim")
require("goxls").setup()
```

`setup()` uses `vim.lsp.config` and `vim.lsp.enable` on Neovim 0.11+, and an autocommand with
`vim.lsp.start` on older versions. It also attaches to `.gox` buffers that were opened before
the plugin loaded. goxls only serves `.gox` buffers, so keep your existing gopls setup for `.go`
files.

### Tree-sitter

The grammar is in [`tree-sitter-gox`](tree-sitter-gox).

- **Building:** on first use, `setup()` compiles the parser with `$CC`, `cc`, `gcc`, `clang` or
  `zig` into `editors/nvim/parser/gox.so`. It recompiles automatically after a plugin update
  changes the grammar. Nvim-treesitter isn't required.
- **Highlighting:** starts on every `gox` buffer.
- **Manual rebuild:** run `:GoxBuildParser`.
- **Opting out:** pass `treesitter = false` to keep the regex syntax.

If you manage parsers with nvim-treesitter instead, point it at `editors/tree-sitter-gox` and
copy `editors/tree-sitter-gox/queries/highlights.scm` to `queries/gox/highlights.scm` on your
runtimepath.

## Helix

`~/.config/helix/languages.toml`:

```toml
[[language]]
name = "gox"
scope = "source.gox"
file-types = ["gox"]
roots = ["go.mod"]
comment-token = "//"
indent = { tab-width = 4, unit = "\t" }
language-servers = ["goxls"]
grammar = "gox"

[language-server.goxls]
command = "goxls"

[[grammar]]
name = "gox"
source = { git = "https://github.com/rknit/tui.gox", rev = "<commit sha>", subpath = "editors/tree-sitter-gox" }
```

Then run `hx --grammar fetch && hx --grammar build`, and copy
`editors/tree-sitter-gox/queries/highlights.scm` to `~/.config/helix/runtime/queries/gox/`.

## Emacs (eglot)

```elisp
(define-derived-mode gox-mode go-mode "Gox")
(add-to-list 'auto-mode-alist '("\\.gox\\'" . gox-mode))
(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs '(gox-mode "goxls")))
```

## Other clients

Start `goxls` over stdio for `*.gox` files with the project root set to the directory that
contains `go.mod`. The language ID doesn't matter.
