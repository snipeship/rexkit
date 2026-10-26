-- rexkit scrollback viewer: a read-only, colour-faithful view of a pane's
-- scrollback with Vim search, motions, visual selection and clipboard yanks.
--
-- `rexkit scrollback` starts `nvim -u <this file>` and passes its settings as
-- JSON in $REXKIT_SCROLLBACK:
--   file    the sanitized capture: text with SGR colours only, "\n" line ends
--   start   "bottom" | "top" | "page-up" | "half-up"
--   dropped how many of the oldest lines Go already left out (its first line
--           is then a note saying so)
--   dark    true for the dark palette
--   colors  rexkit palette tokens as "#rrggbb": fg muted
--           subtle, term_fg and well (the pane's own colours), tray and
--           keycap (the status line), accent accent_solid on_accent, ok err,
--           and sel sel_fg find flash (selection, match and yank fills on the well)
--
-- The capture is fed into a terminal buffer (nvim_open_term), so nvim renders
-- its colours exactly like the pane did: ANSI colours stay indexed and use the
-- host terminal's palette, true colours stay true.
--
-- Needs Neovim 0.10 or newer.

local cfg = vim.json.decode(vim.env.REXKIT_SCROLLBACK or "{}")
local c = cfg.colors or {}
local MAX_LINES = 100000 -- the most rows of scrollback a terminal buffer keeps
local LARGE = 1024 * 1024 -- captures big enough to say "loading…" first

local o = vim.o
o.termguicolors = true
o.background = cfg.dark == false and "light" or "dark"
o.swapfile = false
o.undofile = false
o.shadafile = "NONE"
o.modeline = false
o.clipboard = "unnamedplus"
o.mouse = "a"
o.number = false
o.relativenumber = false
o.signcolumn = "no"
o.foldenable = false
o.cursorline = false
o.scrolloff = 0
o.ignorecase = true
o.smartcase = true
o.incsearch = true
o.hlsearch = true
o.showmode = false
o.showcmd = false
o.ruler = false
o.cmdheight = 0
o.laststatus = 2
o.timeoutlen = 400
o.shortmess = "aoOstTWIcCF"
o.report = 1000000 -- the status line says what was copied
o.fillchars = "eob: "
o.scrollback = MAX_LINES

local function hl(name, spec) vim.api.nvim_set_hl(0, name, spec) end
local tray = c.tray
-- The capture sits on the pane's own colours, so opening the viewer does not
-- flash; floats have no background of their own: paint one.
hl("Normal", { fg = c.term_fg, bg = c.well })
hl("NormalNC", { fg = c.term_fg, bg = c.well })
hl("StatusLine", { fg = c.fg, bg = tray })
hl("StatusLineNC", { fg = c.muted, bg = tray })
hl("RexkitTitle", { fg = c.fg, bg = tray, bold = true })
hl("RexkitKey", { fg = c.fg, bg = c.keycap })
hl("RexkitLabel", { fg = c.muted, bg = tray })
hl("RexkitMeta", { fg = c.subtle, bg = tray })
hl("RexkitMatch", { fg = c.accent, bg = tray, bold = true })
hl("RexkitOK", { fg = c.ok, bg = tray, bold = true })
hl("RexkitErr", { fg = c.err, bg = tray, bold = true })
-- A selection looks as it does in the pane itself: Rex's own selection.
hl("Visual", { fg = c.sel_fg, bg = c.sel })
hl("Search", { fg = c.fg, bg = c.find })
hl("CurSearch", { fg = c.on_accent, bg = c.accent_solid, bold = true })
hl("IncSearch", { link = "CurSearch" })
hl("YankFlash", { fg = c.fg, bg = c.flash })
hl("MsgArea", { fg = c.muted })
hl("ErrorMsg", { fg = c.err, bold = true })

-- Status line ------------------------------------------------------------

local total = 0  -- content lines, known once the capture is loaded
local flash = nil -- {text, group}: a short-lived note such as "✓ copied 3 lines"
local failure = nil

local function seg(text, group) return { text = text, group = group } end

local function render(segs)
  local out = {}
  for _, s in ipairs(segs) do
    out[#out + 1] = "%#" .. s.group .. "#" .. s.text:gsub("%%", "%%%%")
  end
  return table.concat(out)
end

local function width(segs)
  local w = 0
  for _, s in ipairs(segs) do w = w + vim.fn.strdisplaywidth(s.text) end
  return w
end

local function search_count()
  if vim.v.hlsearch == 0 or vim.fn.getreg("/") == "" then return nil end
  local ok, sc = pcall(vim.fn.searchcount, { maxcount = 9999, timeout = 40 })
  if not ok or not sc.total or sc.total == 0 then return nil end
  local of = sc.incomplete == 2 and (sc.maxcount .. "+") or tostring(sc.total)
  return sc.current .. "/" .. of
end

-- The viewer opens on the newest output, so the hint is the search upwards
-- (Vim's ?); from the top it is the usual /. Both work either way.
local search_hint = cfg.start == "top" and { "/", "search" } or { "?", "search up" }
local hints = { search_hint, { "v", "select" }, { "y", "copy" }, { "q", "close" } }
local GAP = 3 -- cells kept free between the hints and the right segment

function _G.RexkitScrollbackStatus()
  local avail = vim.api.nvim_win_get_width(0)
  local left = { seg(" Scrollback", "RexkitTitle") }
  local right = {}
  if failure then
    right[#right + 1] = seg("✗ " .. failure .. "  ", "RexkitErr")
  elseif flash then
    right[#right + 1] = seg(flash[1] .. "  ", flash[2])
  else
    local count = search_count()
    if count then right[#right + 1] = seg("match " .. count .. "  ", "RexkitMatch") end
  end
  local line = vim.fn.line(".")
  right[#right + 1] = seg(line .. "/" .. math.max(total, line) .. " ", "RexkitMeta")

  local budget = avail - width(left) - width(right) - GAP
  for _, h in ipairs(hints) do
    local item = { seg("   ", "RexkitLabel"), seg(" " .. h[1] .. " ", "RexkitKey"), seg(" " .. h[2], "RexkitLabel") }
    local need = width(item)
    if need > budget then break end
    vim.list_extend(left, item)
    budget = budget - need
  end
  return render(left) .. "%#StatusLine#%=" .. render(right)
end

o.statusline = "%!v:lua.RexkitScrollbackStatus()"

local function show(text, group, ms)
  flash = { text, group }
  vim.cmd.redrawstatus()
  local note = flash
  vim.defer_fn(function()
    if flash == note then
      flash = nil
      vim.cmd.redrawstatus()
    end
  end, ms)
end

-- Keys -------------------------------------------------------------------

local function close() vim.cmd("qa!") end
local function map(lhs, rhs, desc) vim.keymap.set("n", lhs, rhs, { silent = true, desc = desc }) end

map("q", close, "Close the scrollback")
map("<Esc><Esc>", close, "Close the scrollback")
map("<C-c>", close, "Close the scrollback")
map("<Esc>", "<cmd>nohlsearch<CR>", "Clear the search highlight")

-- The capture is a picture of the pane, not a live terminal: never enter
-- terminal mode, whatever the key or click.
for _, lhs in ipairs({ "i", "I", "a", "A", "<Insert>" }) do map(lhs, "<Nop>") end
vim.api.nvim_create_autocmd("TermEnter", {
  callback = function() vim.schedule(function() vim.cmd.stopinsert() end) end,
})

-- Soft wraps ---------------------------------------------------------------
-- A terminal buffer breaks long lines into rows. Copies should not: yanks
-- rejoin rows that continue the row above, found by matching the rows
-- against the capture's own lines (from the bottom, as the terminal may have
-- dropped rows at the top).

local page = "" -- the capture as loaded (after fit)
local logical = nil -- its lines without colours, built on the first copy
local wraps = { tick = -1, rows = {} }

local function strip(line) return (line:gsub("\27%[[%d;:]*m", "")) end

local function lines_of(text)
  local out = {}
  for line in (text .. "\n"):gmatch("(.-)\n") do
    out[#out + 1] = strip(line)
  end
  return out
end

-- continued returns the set of rows that continue the row above.
local function continued()
  local buf = vim.api.nvim_get_current_buf()
  local tick = vim.api.nvim_buf_get_changedtick(buf)
  if wraps.tick == tick then return wraps.rows end
  logical = logical or lines_of(page)
  local rows = vim.api.nvim_buf_get_lines(buf, 0, total, false)
  local cont, r = {}, #rows
  for i = #logical, 1, -1 do
    if r < 1 then break end
    local want, first, text = logical[i], r, rows[r]
    while #text < #want and first > 1 do
      first = first - 1
      text = rows[first] .. text
    end
    if text ~= want then break end -- out of step: trust only what matched
    for k = first + 1, r do cont[k] = true end
    r = first - 1
  end
  wraps = { tick = tick, rows = cont }
  return cont
end

-- rejoin merges the yanked pieces of wrapped lines; first is the row of
-- pieces[1].
local function rejoin(pieces, first)
  local cont, out = continued(), { pieces[1] }
  for i = 2, #pieces do
    if cont[first + i - 1] then
      out[#out] = out[#out] .. pieces[i]
    else
      out[#out + 1] = pieces[i]
    end
  end
  return out
end

vim.api.nvim_create_autocmd("TextYankPost", {
  callback = function()
    local ev = vim.v.event
    if ev.operator ~= "y" then return end
    -- vim.hl is Neovim 0.11+ (0.10 has vim.highlight); the flash is a nicety
    -- that must never stop the rejoin below.
    local flasher = vim.hl or vim.highlight
    if flasher then pcall(flasher.on_yank, { higroup = "YankFlash", timeout = 160 }) end
    local lines = ev.regcontents
    if ev.regtype:sub(1, 1) ~= "\22" then -- blockwise copies stay as they are
      lines = rejoin(ev.regcontents, vim.fn.line("'["))
      if #lines < #ev.regcontents then
        local reg = ev.regname
        if reg == "" then reg = o.clipboard:find("unnamedplus") and "+" or '"' end
        vim.fn.setreg(reg, lines, ev.regtype)
      end
    end
    local n = #lines
    show(n == 1 and "✓ copied 1 line" or ("✓ copied " .. n .. " lines"), "RexkitOK", 1800)
  end,
})

-- Y copies the whole line under the cursor, however many rows it wraps to.
map("Y", function()
  local cont, first, last = continued(), vim.fn.line("."), vim.fn.line(".")
  while cont[first] do first = first - 1 end
  while cont[last + 1] do last = last + 1 end
  vim.cmd(first .. "," .. last .. "yank")
end, "Copy this line")

-- Loading ----------------------------------------------------------------

local function read(path)
  local f, err = io.open(path or "", "rb")
  if not f then return nil, err end
  local data = f:read("a")
  f:close()
  return data
end

-- Row budget ---------------------------------------------------------------
-- 'scrollback' counts terminal rows, not lines: a line wider than the window
-- takes several rows, and the terminal silently drops the oldest rows past
-- the limit. So fit keeps the newest lines that surely fit and says how many
-- older ones are not shown, in the same dim note Go uses for its own cap.

local function omitted(n)
  return "\27[2;3m… " .. n .. (n == 1 and " line" or " lines") .. " of older output not shown\27[m"
end

-- rows_of is how many rows line takes at cols columns, or an upper bound.
local function rows_of(line, cols)
  local text = strip(line)
  if #text <= cols then return 1 end -- never wider than its bytes
  if not text:find("[\128-\255]") then return math.ceil(#text / cols) end
  -- A wide character that does not fit at the end of a row moves to the next
  -- one, so a row may hold one cell less.
  return math.ceil(vim.fn.strdisplaywidth(text) / math.max(1, cols - 1))
end

-- fit returns data trimmed to the row budget, with the note as its first line
-- when anything older is left out.
local function fit(data)
  local older = math.max(0, math.floor(tonumber(cfg.dropped) or 0))
  local cols = math.max(1, vim.api.nvim_win_get_width(0))
  -- The buffer keeps MAX_LINES rows plus a screen; leave a screen and a row
  -- spare.
  local budget = MAX_LINES - o.lines - 1
  local _, breaks = data:gsub("\n", "")
  -- Every line takes at most bytes/(cols-1) + 1 rows: most captures are known
  -- to fit without measuring a single line.
  if breaks + 1 + #data / math.max(1, cols - 1) <= budget then return data end

  local lines = vim.split(data, "\n", { plain = true })
  local first = older > 0 and 2 or 1 -- lines[1] is Go's note when it dropped some
  budget = budget - rows_of(omitted(older + #lines), cols) -- room for our note
  local used, keep = 0, #lines + 1
  while keep > first do
    local rows = rows_of(lines[keep - 1], cols)
    if used + rows > budget then break end
    used, keep = used + rows, keep - 1
  end
  if keep == first then return data end -- it all fits after all
  return omitted(older + keep - first) .. "\n" .. table.concat(lines, "\n", keep)
end

local function keys(seq)
  vim.cmd.normal({ vim.api.nvim_replace_termcodes(seq, true, false, true), bang = true })
end

local function place(start)
  local last = math.max(1, vim.fn.prevnonblank(vim.fn.line("$")))
  total = last
  if start == "top" then
    keys("gg")
    return
  end
  vim.api.nvim_win_set_cursor(0, { last, 0 })
  keys("zb")
  if start == "page-up" then
    keys("<C-b>")
  elseif start == "half-up" then
    keys("<C-u>")
  end
end

local function load()
  local data, err = read(cfg.file)
  if not data then
    failure = "cannot read the capture: " .. tostring(err)
    return
  end
  data = fit((data:gsub("\n$", ""))) -- keep the cursor on the last line, not below it
  page = data
  local buf = vim.api.nvim_get_current_buf()
  local chan = vim.api.nvim_open_term(buf, {})
  vim.bo[buf].scrollback = MAX_LINES
  if data ~= "" then
    if #data > LARGE then
      flash = { "loading…", "RexkitLabel" } -- the terminal needs ~1 s for 8 MB
      vim.cmd.redraw()
    end
    -- No pty translates line ends here, so send what a terminal expects.
    local crlf, breaks = data:gsub("\n", "\r\n")
    local tick = vim.api.nvim_buf_get_changedtick(buf)
    vim.api.nvim_chan_send(chan, crlf)
    -- The terminal parses the data at once but fills the buffer on its next
    -- refresh; wait for that before placing the cursor.
    local want = math.min(breaks + 1, MAX_LINES)
    vim.wait(5000, function()
      return vim.api.nvim_buf_get_changedtick(buf) ~= tick and vim.api.nvim_buf_line_count(buf) >= want
    end, 5)
    flash = nil
  end
  place(cfg.start)
  vim.api.nvim_exec_autocmds("User", { pattern = "RexkitScrollbackLoaded" })
end

vim.api.nvim_create_autocmd("VimEnter", { once = true, callback = load })
