-- goxls integration for Neovim: filetype, tree-sitter highlighting and LSP.
--
--   require("goxls").setup({
--     cmd = { "goxls" },            -- or { "goxls", "-gopls", "/path/to/gopls" }
--     filetypes = { "gox" },
--     generate_on_save = true,      -- write foo_gox.go when foo.gox is saved
--     treesitter = true,            -- build and use the bundled tree-sitter parser
--     settings = {},                -- forwarded to goxls' gopls, e.g. { gopls = { gofumpt = true } }
--     on_attach = nil,
--     capabilities = nil,
--   })
local M = {}

local uv = vim.uv or vim.loop

-- editors/nvim (this plugin's runtime directory).
local plugin_dir = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p:h:h:h")
local grammar_src = vim.fs.joinpath(plugin_dir, "..", "tree-sitter-gox", "src")
local parser_path = vim.fs.joinpath(plugin_dir, "parser", "gox.so")

local function mtime(path)
  local st = uv.fs_stat(path)
  return st and st.mtime.sec or nil
end

--- Whether the compiled parser exists and is newer than the grammar.
function M.parser_ready()
  local so, c = mtime(parser_path), mtime(vim.fs.joinpath(grammar_src, "parser.c"))
  return so ~= nil and (c == nil or so >= c)
end

local function start_ts(buf)
  if vim.api.nvim_buf_is_valid(buf) and vim.bo[buf].filetype == "gox" then
    pcall(vim.treesitter.start, buf, "gox")
  end
end

--- Compile the bundled tree-sitter parser into editors/nvim/parser/gox.so.
--- Runs asynchronously; on_done(ok, err) is called on the main loop.
function M.build_parser(on_done)
  on_done = on_done or function(ok, err)
    if ok then
      vim.notify("goxls: built tree-sitter parser for gox", vim.log.levels.INFO)
    else
      vim.notify("goxls: building tree-sitter parser failed: " .. err, vim.log.levels.WARN)
    end
  end
  local cc = vim.env.CC
  if not cc or cc == "" then
    for _, c in ipairs({ "cc", "gcc", "clang", "zig" }) do
      if vim.fn.executable(c) == 1 then
        cc = c
        break
      end
    end
  end
  if not cc then
    on_done(false, "no C compiler found (set $CC)")
    return
  end
  vim.fn.mkdir(vim.fs.dirname(parser_path), "p")
  local cmd = { cc, "-o", parser_path, "-shared", "-fPIC", "-Os", "-I", grammar_src, vim.fs.joinpath(grammar_src, "parser.c") }
  if cc == "zig" then
    table.insert(cmd, 2, "cc")
  end
  vim.system(cmd, { text = true }, function(res)
    vim.schedule(function()
      if res.code ~= 0 then
        on_done(false, res.stderr ~= "" and res.stderr or ("exit " .. res.code))
        return
      end
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
        start_ts(buf)
      end
      on_done(true)
    end)
  end)
end

local function setup_treesitter()
  vim.api.nvim_create_user_command("GoxBuildParser", function()
    M.build_parser()
  end, { desc = "Build the gox tree-sitter parser" })

  vim.api.nvim_create_autocmd("FileType", {
    pattern = "gox",
    group = vim.api.nvim_create_augroup("goxls_treesitter", { clear = true }),
    callback = function(args)
      start_ts(args.buf)
    end,
  })
  if not M.parser_ready() then
    M.build_parser(function(ok, err)
      if not ok then
        vim.notify("goxls: tree-sitter parser unavailable (" .. err .. "); using regex syntax", vim.log.levels.WARN)
      end
    end)
  end
  -- Buffers opened before setup ran (lazy loading).
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    start_ts(buf)
  end
end

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
  opts = opts or {}
  vim.filetype.add({ extension = { gox = "gox" } })
  if not vim.tbl_contains(vim.opt.rtp:get(), plugin_dir) then
    vim.opt.rtp:append(plugin_dir)
  end
  if opts.treesitter ~= false then
    setup_treesitter()
  end

  local cfg = M.config(opts)
  if vim.lsp.config and vim.lsp.enable then -- Neovim 0.11+
    vim.lsp.config("goxls", cfg)
    vim.lsp.enable("goxls")
  else -- Neovim 0.9/0.10
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

  -- Buffers that were opened before the plugin loaded: detect them again so
  -- the filetype, syntax and LSP apply.
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(buf) and vim.api.nvim_buf_get_name(buf):match("%.gox$") and vim.bo[buf].filetype ~= "gox" then
      vim.api.nvim_buf_call(buf, function()
        vim.cmd("filetype detect")
      end)
    end
  end
end

return M
