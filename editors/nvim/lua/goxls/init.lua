-- goxls integration for Neovim.
--
--   require("goxls").setup({
--     cmd = { "goxls" },            -- or { "goxls", "-gopls", "/path/to/gopls" }
--     filetypes = { "gox" },        -- add "go" to let goxls serve .go files too
--     generate_on_save = true,      -- write foo_gox.go when foo.gox is saved
--     settings = {},                -- forwarded to gopls (workspace/configuration)
--     on_attach = nil,
--     capabilities = nil,
--   })
local M = {}

function M.config(opts)
  opts = opts or {}
  local generate = opts.generate_on_save
  if generate == nil then
    generate = true
  end
  return {
    name = "goxls",
    cmd = opts.cmd or { "goxls" },
    filetypes = opts.filetypes or { "gox" },
    root_markers = { "go.mod", "go.work", ".git" },
    init_options = { goxls = { generateOnSave = generate } },
    settings = opts.settings or {},
    on_attach = opts.on_attach,
    capabilities = opts.capabilities,
  }
end

function M.setup(opts)
  vim.filetype.add({ extension = { gox = "gox" } })
  local cfg = M.config(opts)

  if vim.lsp.config and vim.lsp.enable then -- Neovim 0.11+
    vim.lsp.config("goxls", cfg)
    vim.lsp.enable("goxls")
    return
  end

  -- Neovim 0.9/0.10.
  vim.api.nvim_create_autocmd("FileType", {
    pattern = cfg.filetypes,
    group = vim.api.nvim_create_augroup("goxls", { clear = true }),
    callback = function(args)
      local root = vim.fs.root and vim.fs.root(args.buf, cfg.root_markers)
        or vim.fs.dirname(vim.fs.find(cfg.root_markers, { upward = true, path = vim.api.nvim_buf_get_name(args.buf) })[1])
      vim.lsp.start(vim.tbl_extend("force", cfg, { root_dir = root }), { bufnr = args.buf })
    end,
  })
end

return M
