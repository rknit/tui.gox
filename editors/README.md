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

**Not supported for `.gox` files:** formatting and semantic tokens. For syntax highlighting, use
the bundled Vim syntax or your editor's Go highlighting.

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
- Syntax highlighting that layers tags and attributes on top of Vim's Go syntax.
- An ftplugin.
- An LSP setup.

With [lazy.nvim](https://github.com/folke/lazy.nvim):

```lua
{
  "rknit/tui.gox",
  config = function(plugin)
    vim.opt.rtp:append(plugin.dir .. "/editors/nvim")
    require("goxls").setup({
      -- cmd = { "goxls", "-gopls", "/path/to/gopls" },
      -- generate_on_save = true,
      -- on_attach = function(client, bufnr) ... end,
      -- capabilities = require("cmp_nvim_lsp").default_capabilities(),
    })
  end,
}
```

To set it up manually, add the directory to `runtimepath`:

```lua
vim.opt.rtp:append("/path/to/tui.gox/editors/nvim")
require("goxls").setup()
-- Neovim 0.11+ also picks up editors/nvim/lsp/goxls.lua, so this works instead:
-- vim.lsp.enable("goxls")
```

`setup()` uses `vim.lsp.config` and `vim.lsp.enable` on Neovim 0.11+, and an autocommand with
`vim.lsp.start` on older versions. goxls only serves `.gox` buffers, so keep your existing gopls
setup for `.go` files.

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
grammar = "go"

[language-server.goxls]
command = "goxls"
```

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
