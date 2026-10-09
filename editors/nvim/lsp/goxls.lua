-- Default config for vim.lsp.enable("goxls") (Neovim 0.11+).
return {
  cmd = { "goxls" },
  filetypes = { "gox" },
  root_markers = { "go.mod", "go.work", ".git" },
  init_options = { goxls = { generateOnSave = true } },
}
