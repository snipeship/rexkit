-- Returns what a window's tiled panes show, for the backdrop a floating tool
-- draws behind its card: each visible terminal pane's rect (fractions of the
-- window), size, the tail of its content as escape sequences (format vt),
-- long enough to hold the pane's last `rows` lines (its screen), and what its
-- header shows: its title and program.
-- Args: session, window: a window ID, a block ID (the window holding that
-- block, e.g. the tool's own float) or empty (the active window).
-- Run it attached to the session (rex do -s SESSION).
--
-- Floating layers (the tool's own among them) are left out. A zoomed window
-- shows only its zoomed block. A pane that cannot be read is listed with an
-- error; only a missing session or window fails the whole script.
local TERMINAL = "com.superlogical.terminal"
local a = rex.args

local function method(block, name, args)
  return rex.call("block.method", {session_id = a.session, block_id = block, method = name, args = args or {}})
end

local view, err = rex.call("session.view", {session_id = a.session})
if not view then return {error = tostring(err)} end
local want = (a.window ~= nil and a.window ~= "") and a.window or view.active_window_id
local window
for _, w in ipairs(view.windows or {}) do
  if w.window_id == want then window = w end
  for _, l in ipairs(w.layers or {}) do
    for _, b in ipairs(l.blocks or {}) do
      if b.block_id == want then window = w end
    end
  end
end
if not window then return {error = "window " .. tostring(want) .. " not found"} end

local tiled
for _, l in ipairs(window.layers or {}) do
  if l.kind == "tiled" then tiled = l end
end
local out = {window_id = window.window_id, zoomed = window.zoomed_block_id, panes = {}}
if not tiled or not tiled.layout then return out end
local terminal = {}
for _, b in ipairs(tiled.blocks or {}) do terminal[b.block_id] = b.creator_name == TERMINAL end

-- tail returns about the last `need` bytes of the block's vt content, from a
-- line boundary when it had to cut (see tail.lua: format keeps one byte
-- inline and spools the rest as a blob, read here from the end). It doubles
-- the read until it holds `rows` line breaks or the whole content.
local function tail(block, rows, need)
  local first, ferr = method(block, "format", {format = "vt", max_bytes = 1})
  if not first then return nil, ferr end
  if not first.id then return first.content or "" end
  local total = first.total or 0
  for _ = 1, 4 do
    local offset = math.max(0, total - need)
    local parts, at = {}, offset
    for _ = 1, 64 do
      local blob, berr = method(block, "blob_read", {id = first.id, offset = at, max_bytes = 983040})
      if not blob then return nil, berr end
      parts[#parts + 1] = blob.data
      at = at + #blob.data
      if blob.eof or #blob.data == 0 then break end
    end
    local content = table.concat(parts)
    local _, breaks = content:gsub("\n", "")
    if offset == 0 then return content end
    if breaks >= rows then
      return content:sub((content:find("\n", 1, true) or 0) + 1) -- drop the partial first line
    end
    need = need * 4
  end
  return nil, "screen too large"
end

-- program is the name of what runs in the terminal, as it was started
-- (Claude Code's process is named after its version; it runs as "claude"),
-- else the title's owner. login (the terminal's own child, which is all a
-- pane running a plain command shows) names nothing.
local function program(block, title)
  local p = method(block, "process")
  local fg = p and p.foreground
  local name = fg and ((fg.invoked_path or fg.path or ""):match("([^/]+)$") or fg.name)
  if name == nil or name == "login" then name = title and title.owner and title.owner.name end
  if name == "login" then return nil end
  return name
end

local function add(block, rect)
  if not terminal[block] then return end
  local title = method(block, "title")
  local pane = {block_id = block, rect = rect, title = title and title.title or nil, program = program(block, title)}
  local size, serr = method(block, "size")
  if not size then
    pane.error = tostring(serr)
  else
    pane.size = size
    local rows, cols = size.rows or 0, size.columns or 0
    local content, cerr = tail(block, rows, math.max(16384, rows * (cols * 6 + 32)))
    if content then pane.screen = content else pane.error = tostring(cerr) end
  end
  out.panes[#out.panes + 1] = pane
end

local function walk(node)
  if node.split then
    walk(node.split.before)
    walk(node.split.after)
  elseif node.block_id then
    add(node.block_id, node.rect)
  end
end

if window.zoomed_block_id and window.zoomed_block_id ~= "" then
  add(window.zoomed_block_id, {0, 0, 1, 1})
else
  walk(tiled.layout)
end
return out
