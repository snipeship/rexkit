-- Live checks for rexkit's Lua layer. Run them ONLY against an isolated Rex
-- server: it creates and destroys sessions. Setup (see lua/tests/README.md):
--   * the server's config is `return dofile("<rexkit>/lua/init.lua")`;
--   * $XDG_CONFIG_HOME/rexkit/settings.lua sets `binary` to the stand-in tool
--     built from lua/tests/standin (tools answer the handshake keys, `await` waits for result.json);
--   * the server runs with STANDIN_LOG=<file>, where the stand-in appends the
--     argv of every tool it runs, and with REX_SERVER=<its own address>: a
--     server gives its blocks no REX_SERVER, and without it the stand-in
--     refuses to run rather than call the default (your) server.
--   rex -S <server> do lua/tests/check.lua --json --args '{"isolated":true,"log":"<file>","rex":"<rex binary>","server":"<server>"}'
--   ("rex" and "server" enable the double-press check, which runs two rex
--   processes at once.)
-- The result is {ok, passed = {...}, failed = {name = error}}.
assert(rex.args.isolated == true, "pass --args '{\"isolated\":true,...}': this script creates and destroys sessions")
local TERMINAL = "com.superlogical.terminal"
local SHELL = TERMINAL .. ".shell"
local PREFIX = "rexkit-test"

local function call(method, args)
  local result, err = rex.call(method, args or {})
  assert(not err, method .. ": " .. tostring(err))
  return result or {}
end

for _, s in ipairs(call("session.list").sessions or {}) do
  assert((s.label or ""):sub(1, #PREFIX) == PREFIX, "refusing to run: session " .. tostring(s.label) .. " exists, so this is not an isolated server")
end

-- Helpers -----------------------------------------------------------------------

local function cat(label) return rex.layout.block { flavor = SHELL, label = label, options = { shell = "none", command = { "/bin/cat" } } } end

local sessions = {}
local function new_session(name, layout)
  local created = call("session.create", { label = PREFIX .. "-" .. name,
    initial_windows = { { window_label = "home", layout = layout or cat("A") } } })
  sessions[#sessions + 1] = created.session_id
  call("session.attach", { session_id = created.session_id })
  return created.session_id, created.initial_windows[1]
end

local function view(sid) return call("session.view", { session_id = sid }) end

local function walk(node, fn)
  if not node then return end
  if node.block_id then return fn(node) end
  walk(node.split.before, fn)
  walk(node.split.after, fn)
end

-- find returns {rect, window, layer, label} for a block, or nil.
local function find(v, id)
  for _, w in ipairs(v.windows or {}) do
    for _, l in ipairs(w.layers or {}) do
      local found
      walk(l.layout, function(node) if node.block_id == id then found = node end end)
      if found then
        local label
        for _, b in ipairs(l.blocks or {}) do if b.block_id == id then label = b.label end end
        return { rect = table.concat(found.rect, ","), window = w, layer = l, label = label }
      end
    end
  end
end

local function window_of(sid, id) local f = find(view(sid), id); return f and f.window.window_id end
local function rect_of(sid, id) local f = find(view(sid), id); return f and f.rect end

-- blocks_in lists a window's blocks ({block_id, label}) in all its layers.
local function blocks_in(sid, window_id)
  local list = {}
  for _, w in ipairs(view(sid).windows) do
    if w.window_id == window_id then
      for _, l in ipairs(w.layers) do
        for _, b in ipairs(l.blocks) do list[#list + 1] = b end
      end
    end
  end
  return list
end

local function holds(sid)
  local n = 0
  for _, w in ipairs(view(sid).windows) do
    for _, l in ipairs(w.layers) do
      for _, b in ipairs(l.blocks) do if b.label == "rexkit:hold" then n = n + 1 end end
    end
  end
  return n
end

local function focused(sid)
  local v = view(sid)
  for _, w in ipairs(v.windows) do
    if w.window_id == v.active_window_id then return w.focused_block_id, w.zoomed_block_id end
  end
end

local function floats(sid)
  local list = {}
  for _, w in ipairs(view(sid).windows) do
    for _, l in ipairs(w.layers) do
      if l.kind == "floating" then list[#list + 1] = { layer = l, block = l.blocks[1] } end
    end
  end
  return list
end

local function wait_until(what, fn, seconds)
  local deadline = os.time() + (seconds or 3)
  while true do
    local value = fn()
    if value then return value end
    assert(os.time() <= deadline, "timed out waiting for " .. what)
    rex.sleep(0.02)
  end
end

local function pid(sid, block)
  return wait_until("a process in " .. block, function()
    local p = rex.call(TERMINAL .. ".process", { session_id = sid, block_id = block, args = {} })
    return p and p.child and p.child.pid
  end)
end

-- act runs an action the way a key in the app does and returns its
-- response: {result, client_actions}.
local function act(sid, block, action, args)
  local res, err = rex.call("client.action", { session_id = sid, block_id = block, action = action, args = args, origin = "key" })
  assert(res, action .. ": " .. tostring(err))
  return res
end

local function queued(res)
  local names = {}
  for i, a in ipairs(res.client_actions or {}) do names[i] = a.action end
  return table.concat(names, " ")
end

local function queued_args(res, name)
  for _, a in ipairs(res.client_actions or {}) do if a.action == name then return a.args or {} end end
end

local function type_into(sid, block, text)
  call(TERMINAL .. ".write", { session_id = sid, block_id = block, args = { data = text } })
end

local function screen(sid, block)
  local f = call(TERMINAL .. ".format", { session_id = sid, block_id = block, args = { format = "text" } })
  return f.content or ""
end

local function tool_argv()
  local file = io.open(rex.args.log or "", "r")
  if not file then return nil end
  local last
  for line in file:lines() do last = line end
  file:close()
  return last and rex.json.decode(last)
end

local function flag(argv, name)
  for i, arg in ipairs(argv or {}) do if arg == name then return argv[i + 1] end end
end

local function has(argv, name)
  for _, arg in ipairs(argv or {}) do if arg == name then return true end end
  return false
end

local function eq(a, b, what) assert(a == b, ("%s: got %s, want %s"):format(what, tostring(a), tostring(b))) end

-- open opens a tool from block and waits until it is ready for keys.
local function open(sid, block, action, args)
  local res = act(sid, block, action, args)
  local tool = assert(res.result and res.result.block_id, "no tool block in " .. rex.json.encode(res))
  wait_until("the tool to start", function() return screen(sid, tool):find("ready", 1, true) end)
  return tool, res
end

local passed, failed = {}, {}
local function check(name, fn)
  local ok, err = pcall(fn)
  if ok then passed[#passed + 1] = name else failed[name] = tostring(err) end
end

-- Keymap -----------------------------------------------------------------------------

local keymap = call("client.keymap", {})
local actions = {}
for _, a in ipairs(keymap.actions or {}) do actions[a.name] = a end
local function binding(mode, key)
  for _, b in ipairs(keymap.bindings) do
    if b.mode == mode and table.concat(b.keys, ">") == key then return b end
  end
end
local modes = {}
for _, m in ipairs(keymap.modes or {}) do modes[m.name] = m end

-- Client actions rexkit binds (from the app's action list).
local CLIENT = {}
for name in ([[client.config.reload client.find.next client.find.open client.find.previous
  client.mode.enter client.mode.exit_all client.palette.toggle client.prompt.next client.prompt.previous
  client.settings client.sidebar.toggle client.tab.close client.tab.move.backward client.tab.move.forward
  client.tab.new client.tab.next client.tab.previous client.window.close pane.balance pane.close pane.focus
  pane.focus_next pane.focus_previous pane.go_to_directory pane.insert_path pane.move.down pane.move.left
  pane.move.right pane.move.up pane.move_to_new_tab pane.move_to_tab pane.send_key pane.split pane.split.down
  pane.split.right pane.zoom session.new session.next session.previous session.select]]):gmatch("%S+") do
  CLIENT[name] = true
end

check("keymap has no diagnostics or duplicates", function()
  eq(#(keymap.diagnostics or {}), 0, "diagnostics")
  local seen = {}
  for _, b in ipairs(keymap.bindings) do
    local id = (b.mode or "") .. "/" .. table.concat(b.keys, ">")
    assert(not seen[id], "bound twice: " .. id)
    seen[id] = true
  end
end)

-- The keys a persistent mode blocks unless it or the root binds them
-- (written out again here, independently of core.lua).
local PERSISTENT = { "pane", "tab", "resize", "move", "scroll", "session" }
local function key_set(mods, bases)
  local set = {}
  for _, mod in ipairs(mods) do for _, base in ipairs(bases) do set[mod .. base] = true end end
  return set
end
local TEXT = {}
for c = 97, 122 do TEXT[#TEXT + 1] = string.char(c) end
for c = 48, 57 do TEXT[#TEXT + 1] = string.char(c) end
for _, k in ipairs({ "`", "-", "=", "[", "]", "\\", ";", "'", ",", ".", "/", "space", "tab", "backspace" }) do
  TEXT[#TEXT + 1] = k
end
local BLOCKABLE = key_set({ "", "shift+", "ctrl+", "alt+", "alt+shift+" }, TEXT)
for k in pairs(key_set({ "", "shift+", "ctrl+", "alt+", "alt+shift+" },
  { "delete", "left", "right", "up", "down", "home", "end", "pageup", "pagedown" })) do BLOCKABLE[k] = true end

local function bound_in(mode)
  local set = {}
  for _, b in ipairs(keymap.bindings) do
    if b.mode == mode then set[table.concat(b.keys, ">")] = true end
  end
  return set
end

local function blocked_in(mode)
  local set = {}
  for _, k in ipairs((modes[mode] or {}).blocked or {}) do
    set[type(k) == "table" and table.concat(k, ">") or k] = true
  end
  return set
end

check("modes block exactly the text keys neither they nor the root bind", function()
  local root = bound_in(nil)
  for _, name in ipairs(PERSISTENT) do
    local own, blocked = bound_in(name), blocked_in(name)
    for key in pairs(BLOCKABLE) do
      eq(blocked[key] == true, not own[key] and not root[key], name .. " blocks " .. key)
    end
    for key in pairs(blocked) do
      assert(BLOCKABLE[key], name .. " blocks " .. key .. ", which is not a text key")
      assert(not key:find("cmd", 1, true), name .. " blocks a ⌘ key: " .. key)
      assert(not root[key], name .. " blocks the global " .. key)
    end
  end
  for _, name in ipairs({ "tmux", "locked", "picker" }) do
    eq(next(blocked_in(name)), nil, name .. " blocks keys")
  end
end)

check("global shortcuts still work in pane mode; text keys don't leak", function()
  local blocked, own = blocked_in("pane"), bound_in("pane")
  for _, key in ipairs({ "alt+h", "alt+n", "f1", "ctrl+shift+space", "ctrl+t", "enter", "esc" }) do
    assert(not blocked[key], "pane mode blocks " .. key)
  end
  assert(binding(nil, "alt+h") and binding(nil, "f1") and binding(nil, "ctrl+shift+space"), "the globals exist")
  for _, key in ipairs({ "a", "shift+a", "ctrl+a", "alt+a", "space", "alt+left", "alt+shift+." }) do
    assert(blocked[key] and not own[key], "pane mode lets " .. key .. " through")
  end
end)

check("each mode row has its own title", function()
  -- the app's mode panel titles a row with its action's title only
  for name in pairs(modes) do
    if name ~= "locked" and name ~= "picker" then
      local args_of, action_of = {}, {}
      for _, b in ipairs(keymap.bindings) do
        if b.mode == name then
          local key, args = table.concat(b.keys, ">"), rex.json.encode(b.args or {})
          if args_of[b.action] then
            eq(args, args_of[b.action], name .. ": " .. b.action .. " with other args at " .. key)
          end
          args_of[b.action] = args
          local a = actions[b.action]
          if a and a.kind == "lua" then
            local other = action_of[a.title]
            assert(not other or other == b.action,
              ("%s: %s and %s share the title %q"):format(name, tostring(other), b.action, a.title))
            action_of[a.title] = b.action
          end
        end
      end
    end
  end
  eq(actions.rexkit_grow_left.title, "Grow left", "resize/h title")
  eq(binding("resize", "h").action, "rexkit_grow_left", "resize/h")
  eq(binding("resize", "shift+l").action, "rexkit_shrink_right", "resize/L")
  eq(binding("pane", "j").action, "rexkit_focus_down", "pane/j")
  eq(binding("tab", "3").action, "rexkit_tab_goto_3", "tab/3")
  eq(binding("scroll", "u").action, "rexkit_scrollback_half_up", "scroll/u")
  eq(binding("scroll", "pagedown").action, "rexkit_scrollback_bottom", "scroll/pagedown")
end)

check("every binding names a known action", function()
  for _, b in ipairs(keymap.bindings) do
    local known = actions[b.action] or CLIENT[b.action]
    assert(known, "unknown action " .. b.action .. " for " .. (b.mode or "") .. "/" .. table.concat(b.keys, ">"))
  end
end)

check("no global binding for enter, escape or ctrl+c", function()
  for _, key in ipairs({ "enter", "esc", "escape", "ctrl+c" }) do
    assert(not binding(nil, key), "global " .. key .. " is bound")
  end
end)

check("palette: user actions in rexkit, plumbing in rexkit internal", function()
  local internal = { rexkit_commit = true, rexkit_dismiss = true, rexkit_diagnostics = true,
    rexkit_tmux_unbound = true }
  for name, a in pairs(actions) do
    if a.kind == "lua" then
      eq(a.category, internal[name] and "rexkit internal" or "rexkit", name .. " category")
    end
  end
end)

check("modes: not exclusive (tmux is), with exits and prefix switches", function()
  for _, name in ipairs(PERSISTENT) do
    assert(modes[name] and not modes[name].exclusive, name .. " is exclusive (it would swallow ⌘ and the globals)")
    for _, key in ipairs({ "esc", "enter", "ctrl+c" }) do
      eq(binding(name, key) and binding(name, key).action, "client.mode.exit_all", name .. "/" .. key)
    end
  end
  eq(binding("pane", "ctrl+p").action, "client.mode.exit_all", "pane/ctrl+p")
  eq(binding("pane", "ctrl+t").action, "rexkit_mode_tab", "pane/ctrl+t")
  eq(binding("tab", "ctrl+g").action, "rexkit_mode_locked", "tab/ctrl+g")
  eq(binding(nil, "ctrl+p").action, "client.mode.enter", "ctrl+p")
  eq(binding(nil, "ctrl+p").args.name, "pane", "ctrl+p mode")
  eq(binding(nil, "ctrl+b").args.once, true, "tmux is one-shot")
  assert(modes.tmux.exclusive, "tmux is not exclusive")
  for _, key in ipairs({ "esc", "enter", "ctrl+c" }) do
    eq(binding("tmux", key).action, "client.mode.exit_all", "tmux/" .. key)
  end
  eq(binding("pane", "o").action, "rexkit_files", "pane/o")
  eq(binding("session", "w").action, "rexkit_goto", "session/w")
  eq(binding("resize", "=").action, "rexkit_grow", "resize/=")
  eq(binding("resize", "shift+=").action, "rexkit_grow", "resize/+")
  eq(binding("resize", "-").action, "rexkit_shrink", "resize/-")
  eq(binding("resize", "0").action, "pane.balance", "resize/0")
  eq(binding("pane", "=").action, "rexkit_balance", "pane/=")
end)

check("tmux prefix: every key counts once, prefixes switch, ? opens the guide", function()
  eq(binding("tmux", "ctrl+p").action, "rexkit_mode_pane", "tmux/ctrl+p")
  eq(binding("tmux", "ctrl+g").action, "rexkit_mode_locked", "tmux/ctrl+g")
  eq(binding("tmux", "ctrl+b").action, "pane.send_key", "tmux/ctrl+b sends ctrl+b")
  eq(binding("tmux", "shift+/").action, "rexkit_keys", "tmux/?")
  eq(binding("tmux", "shift+/").args.mode, "tmux", "tmux/? mode")
  eq(binding("tmux", "n").action, "rexkit_tab_next", "tmux/n records the last tab")
  eq(binding("tmux", "p").action, "rexkit_tab_previous", "tmux/p")
  eq(binding("tmux", "h").action, "rexkit_focus_left", "tmux/h")
  local tmux = key_set({ "", "shift+" }, TEXT)
  for c = 97, 122 do tmux["ctrl+" .. string.char(c)] = true end
  for key in pairs(tmux) do assert(binding("tmux", key), "tmux/" .. key .. " is not bound, so it would stick") end
  eq(binding("tmux", "q").action, "rexkit_tmux_unbound", "tmux/q")
  for key in pairs(bound_in(nil)) do assert(binding("tmux", key), "the global " .. key .. " would stick in tmux") end
  eq(binding("tmux", "alt+h").action, "rexkit_tmux_unbound", "tmux/alt+h")
  eq(actions.rexkit_tmux_unbound.category, "rexkit internal", "the catch-all is plumbing (not in the guide)")
end)

check("locked passes every global key through; only ctrl+g leaves", function()
  assert(modes.locked and not modes.locked.exclusive, "locked must not be exclusive (it would swallow typing)")
  for _, b in ipairs(keymap.bindings) do
    if b.mode == nil then
      local key = table.concat(b.keys, ">")
      local locked = binding("locked", key)
      if key == "ctrl+g" then
        eq(locked.action, "client.mode.exit_all", "locked/ctrl+g")
      else
        assert(locked and locked.action == "pane.send_key" and locked.args.key == key, "locked leaks " .. key)
      end
    end
  end
  eq(binding("locked", "esc").action, "pane.send_key", "locked/escape")
end)

check("picker mode binds only the handshake keys", function()
  assert(modes.picker and not modes.picker.exclusive, "picker must not be exclusive")
  local count = 0
  for _, b in ipairs(keymap.bindings) do if b.mode == "picker" then count = count + 1 end end
  eq(count, 4, "picker bindings")
  eq(binding("picker", "enter").action, "rexkit_commit", "picker/enter")
  eq(binding("picker", "esc").action, "rexkit_dismiss", "picker/escape")
  eq(binding("picker", "ctrl+c").action, "rexkit_dismiss", "picker/ctrl+c")
  eq(binding("picker", "ctrl+shift+space").action, "rexkit_goto", "picker/ctrl+shift+space")
end)

-- Keys in panes ------------------------------------------------------------------------

check("enter, escape and ctrl+c reach a pane natively", function()
  local sid, w = new_session("keys")
  local block = w.block_ids[1]
  for _, key in ipairs({
    { key = "enter", mods = "", text = "\r", unshifted_codepoint = 13 },
    { key = "escape", mods = "", text = "\27", unshifted_codepoint = 27 },
    { key = "key_c", mods = "ctrl", text = "\3", unshifted_codepoint = 99 },
  }) do
    key.action, key.consumed_mods, key.composing = "press", "", false
    local res = call("client.key", { session_id = sid, block_id = block, key = key })
    eq(res.handled, false, key.key .. " handled")
  end
end)

check("mode commands queue native client actions", function()
  local sid, w = new_session("modes")
  local b = w.block_ids[1]
  eq(queued(act(sid, b, "rexkit_split_right")), "client.mode.exit_all pane.split", "split right")
  eq(queued_args(act(sid, b, "rexkit_split_right"), "pane.split").direction, "right", "split direction")
  eq(queued(act(sid, b, "rexkit_mode_tab")), "client.mode.exit_all client.mode.enter", "switch to tab mode")
  eq(queued_args(act(sid, b, "rexkit_mode_tmux"), "client.mode.enter").once, true, "tmux once")
  eq(queued(act(sid, b, "rexkit_find")), "client.mode.exit_all client.find.open", "find leaves the mode")
end)

-- Layout ----------------------------------------------------------------------------------

check("alt+l focuses Rex's neighbour; alt+h at the edge switches tab", function()
  -- S | (R1 / (R2a | R2b)): the Lua neighbour search used to pick tiny R2a.
  local sid, w = new_session("focus", rex.layout.horizontal(0.5, cat("S"),
    rex.layout.vertical(0.9, cat("R1"), rex.layout.horizontal(0.1, cat("R2a"), cat("R2b")))))
  local s, r1 = w.block_ids[1], w.block_ids[2]
  call("session.focus_block", { session_id = sid, block_id = s })
  act(sid, s, "rexkit_focus_right_or_tab")
  eq(focused(sid), r1, "alt+l from S")
  local other = call("session.new_window", { session_id = sid, window_label = "two", focus = false, layout = cat("T") })
  call("session.focus_block", { session_id = sid, block_id = s })
  local res = act(sid, s, "rexkit_focus_left_or_tab")
  eq(queued_args(res, "session.select").window_id, other.window_id, "alt+h at the left edge wraps to the last tab")
end)

check("resize grows and shrinks back; swap keeps programs", function()
  local sid, w = new_session("resize", rex.layout.horizontal(0.4, cat("A"), rex.layout.vertical(0.3, cat("B"), cat("C"))))
  local a, b = w.block_ids[1], w.block_ids[2]
  local before, process = rect_of(sid, a), pid(sid, a)
  act(sid, a, "rexkit_grow", { direction = "right" })
  assert(rect_of(sid, a) ~= before, "grow did nothing")
  act(sid, a, "rexkit_shrink", { direction = "right" })
  eq(rect_of(sid, a), before, "shrink undoes grow")
  act(sid, a, "rexkit_grow")
  act(sid, a, "rexkit_shrink")
  eq(rect_of(sid, a), before, "+ then -")
  local res = act(sid, a, "rexkit_swap_next")
  eq(res.result.swapped_with, b, "swap next")
  act(sid, a, "rexkit_swap_previous")
  eq(rect_of(sid, a), before, "swap back")
  eq(pid(sid, a), process, "same program")
end)

check("tabs: next, goto and last tab", function()
  local sid, w = new_session("tabs")
  local two = call("session.new_window", { session_id = sid, window_label = "two", focus = false, layout = cat("T2") })
  local three = call("session.new_window", { session_id = sid, window_label = "three", focus = false, layout = cat("T3") })
  local a = w.block_ids[1]
  call("session.focus_block", { session_id = sid, block_id = a })
  eq(queued_args(act(sid, a, "rexkit_tab_goto", { index = 3 }), "session.select").window_id, three.window_id, "goto 3")
  eq(queued_args(act(sid, three.block_ids[1], "rexkit_last_tab"), "session.select").window_id, w.window_id, "last tab")
  eq(queued_args(act(sid, a, "rexkit_tab_next"), "session.select").window_id, two.window_id, "next tab")
end)

check("pane to next tab, and to a new tab at the end", function()
  local sid, w = new_session("break", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local two = call("session.new_window", { session_id = sid, window_label = "two", focus = false, layout = cat("T2") })
  local a, b = w.block_ids[1], w.block_ids[2]
  local process = pid(sid, a)
  act(sid, a, "rexkit_pane_to_next_tab")
  eq(window_of(sid, a), two.window_id, "moved into the next tab")
  act(sid, a, "rexkit_pane_to_next_tab")
  local new = window_of(sid, a)
  assert(new ~= two.window_id and new ~= w.window_id, "a new tab")
  eq(pid(sid, a), process, "same program")
  assert(window_of(sid, b) == w.window_id, "B stayed")
end)

-- Scratch ---------------------------------------------------------------------------------

local function park(sid, block) return act(sid, block, "rexkit_scratch_park").result end

check("scratch: park and restore a pane with a split sibling exactly", function()
  local sid, w = new_session("scratch", rex.layout.horizontal(0.4, cat("A"), rex.layout.vertical(0.3, cat("B"), cat("C"))))
  local a, b, c = w.block_ids[1], w.block_ids[2], w.block_ids[3]
  local where = { [a] = rect_of(sid, a), [b] = rect_of(sid, b), [c] = rect_of(sid, c) }
  local process = pid(sid, a)
  local parked = park(sid, a)
  assert(parked.parked, "not parked")
  assert(window_of(sid, a) ~= w.window_id, "A is still home")
  local restored = park(sid, a)
  assert(restored.restored, "not restored: " .. rex.json.encode(restored))
  for id, rect in pairs(where) do eq(rect_of(sid, id), rect, "rect of " .. id) end
  eq(pid(sid, a), process, "same program")
end)

check("scratch: a single-pane tab keeps a hold pane; E on it restores", function()
  local sid = new_session("single")
  local single = call("session.new_window", { session_id = sid, window_label = "Single", focus = false, layout = cat("S") })
  local only = single.block_ids[1]
  park(sid, only)
  local hold = blocks_in(sid, single.window_id)[1]
  eq(hold.label, "rexkit:hold", "hold pane label")
  local argv = rex.args.log and wait_until("the hold pane to start", function()
    local last = tool_argv()
    return last and last[2] == "hold" and last
  end)
  if argv then
    eq(flag(argv, "--block"), only, "hold --block")
    assert(flag(argv, "--message"):find("parked", 1, true), "hold message")
    local hints = rex.json.decode(flag(argv, "--hints"))
    eq(hints[1].key, "ctrl+p e", "hint key from the real binding")
  end
  local res = park(sid, hold.block_id)
  assert(res.restored, "E on the hold pane restores")
  eq(window_of(sid, only), single.window_id, "back home")
  assert(not find(view(sid), hold.block_id), "hold pane closed")
end)

check("scratch: closed home tab comes back where it was", function()
  local sid, w = new_session("closedhome")
  local mid = call("session.new_window", { session_id = sid, window_label = "Mid", focus = false, layout = cat("M") })
  call("session.new_window", { session_id = sid, window_label = "Last", focus = false, layout = cat("L") })
  local m = mid.block_ids[1]
  park(sid, m)
  call("session.close_window", { session_id = sid, window_id = mid.window_id })
  local res = park(sid, m)
  assert(res.restored, "restored")
  local v = view(sid)
  eq(v.windows[2].window_id, window_of(sid, m), "restored tab is second again")
  eq(v.windows[2].label, "Mid", "restored tab label")
end)

check("scratch: E on a stale hold pane removes it, never parks it", function()
  local sid = new_session("stale")
  local single = call("session.new_window", { session_id = sid, window_label = "Single", focus = false, layout = cat("S") })
  local only = single.block_ids[1]
  park(sid, only)
  local hold = blocks_in(sid, single.window_id)[1].block_id
  call("block.close", { session_id = sid, block_id = only })
  wait_until("the parked pane to close", function() return not find(view(sid), only) end)
  local res = park(sid, hold)
  assert(res.cleared, "cleared: " .. rex.json.encode(res))
  assert(not find(view(sid), hold), "hold pane closed")
  eq(holds(sid), 0, "hold panes")
end)

check("scratch: E on a pane moved out of Scratch parks it again", function()
  local sid, w = new_session("movedout", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a, b = w.block_ids[1], w.block_ids[2]
  park(sid, a)
  call("session.move_block", { session_id = sid, block_id = a, anchor_block_id = b, direction = "horizontal",
    side = "before", ratio = 0.5 })
  local res = park(sid, a)
  assert(res.parked, "parked again, not restored: " .. rex.json.encode(res))
  -- Scratch closed when its only pane left; parking again made a new one
  local f = find(view(sid), a)
  eq(f.window.label, "Scratch", "in Scratch")
end)

check("scratch: W visits Scratch and comes back", function()
  local sid, w = new_session("toggle")
  local a = w.block_ids[1]
  local res = act(sid, a, "rexkit_scratch").result
  local scratch = res.scratch_window_id
  eq(res.window_id, scratch, "went to Scratch")
  local back = act(sid, view(sid).windows[2].focused_block_id, "rexkit_scratch").result
  eq(back.window_id, w.window_id, "came back")
end)

check("scratch: a double press acts once", function()
  if not (rex.args.rex and rex.args.server) then return end
  local sid, w = new_session("double", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a = w.block_ids[1]
  local req = rex.json.encode({ session_id = sid, block_id = a, action = "rexkit_scratch_park", origin = "key" })
  local argv = { rex.args.rex, "--autostart=false", "-S", rex.args.server, "api", "call", "client.action", req }
  local one, two = rex.exec.start(argv), rex.exec.start(argv)
  local r1, r2 = one:wait(10), two:wait(10)
  local results = r1.stdout .. r2.stdout
  local _, parks = results:gsub('"parked":true', "")
  assert(parks <= 1, "parked twice: " .. results)
  eq(holds(sid), 0, "hold panes")
end)

-- Floating tools and the handshake --------------------------------------------------------

check("goto: float, picker mode, typed-then-enter commit, select queued", function()
  local sid, w = new_session("goto", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a, b = w.block_ids[1], w.block_ids[2]
  call("session.focus_block", { session_id = sid, block_id = a })
  local tool, res = open(sid, a, "rexkit_goto")
  eq(queued(res), "client.mode.exit_all client.mode.enter", "open queues the picker mode")
  eq(queued_args(res, "client.mode.enter").name, "picker", "picker mode")
  local f = floats(sid)
  eq(#f, 1, "one float")
  eq(f[1].block.label, "rexkit:goto", "tool label")
  eq(focused(sid), tool, "tool focused")
  local argv = tool_argv()
  if argv then
    eq(flag(argv, "--origin-block"), a, "--origin-block")
    eq(flag(argv, "--origin-window"), w.window_id, "--origin-window")
    eq(flag(argv, "--session"), sid, "--session")
    assert(flag(argv, "--state"), "--state")
    -- the stand-in's own API calls must reach this server, not the default one
    local owner = rex.json.decode(assert(io.open(flag(argv, "--state") .. "/owner.json")):read("*a"))
    eq(owner.block_id, tool, "owner.json block")
    if rex.args.server then eq(owner.server, rex.args.server, "the stand-in's server") end
  end
  type_into(sid, tool, "goto " .. sid .. " " .. w.window_id .. " " .. b)
  local done = act(sid, tool, "rexkit_commit", { key = "enter" })
  eq(queued(done), "client.mode.exit_all session.select", "commit queues")
  eq(queued_args(done, "session.select").session_id, sid, "select session")
  eq(done.result.block_id, b, "the typed target, not a stale one")
  eq(done.result.commit, true, "the ⏎ came with the commit marker")
  wait_until("the float to close", function() return #floats(sid) == 0 end)
  eq(focused(sid), b, "focus on the target")
end)

check("dismiss restores the exact focus and zoom", function()
  local sid, w = new_session("dismiss", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a = w.block_ids[1]
  call("session.zoom_block", { session_id = sid, block_id = a, zoom = true })
  local tool = open(sid, a, "rexkit_goto")
  eq(has(tool_argv(), "--zoomed"), true, "--zoomed passed")
  local res = act(sid, tool, "rexkit_dismiss", { key = "escape" })
  eq(queued(res), "client.mode.exit_all", "dismiss leaves the mode")
  wait_until("the float to close", function() return #floats(sid) == 0 end)
  local focus, zoom = focused(sid)
  eq(focus, a, "focus back on A")
  eq(zoom, a, "A zoomed again")
end)

check("a tool can keep the mode (stay) and close on ctrl+c", function()
  local sid, w = new_session("stay")
  local a = w.block_ids[1]
  local tool = open(sid, a, "rexkit_goto")
  type_into(sid, tool, "stay")
  local res = act(sid, tool, "rexkit_commit", { key = "enter" })
  eq(queued(res), "", "stay queues nothing")
  eq(#floats(sid), 1, "tool still open")
  eq(res.result.commit, true, "the ⏎ came with the commit marker")
  local argv = tool_argv()
  if argv then eq(io.open(flag(argv, "--state") .. "/commit"), nil, "the marker is gone after the ⏎") end
  type_into(sid, tool, "back")
  eq(queued(act(sid, tool, "rexkit_dismiss", { key = "escape" })), "", "escape that only backs out")
  res = act(sid, tool, "rexkit_dismiss", { key = "ctrl+c" })
  eq(queued(res), "client.mode.exit_all", "ctrl+c closes")
  wait_until("the float to close", function() return #floats(sid) == 0 end)
  eq(focused(sid), a, "focus back")
end)

check("miss: a picker key in another pane goes on to it", function()
  local sid, w = new_session("miss")
  local a = w.block_ids[1]
  local res = act(sid, a, "rexkit_commit", { key = "enter" })
  eq(queued(res), "client.mode.exit_all pane.send_key", "enter forwarded")
  eq(queued_args(res, "pane.send_key").key, "enter", "the same key")
  res = act(sid, a, "rexkit_dismiss", { key = "ctrl+c" })
  eq(queued_args(res, "pane.send_key").key, "ctrl+c", "ctrl+c forwarded")
end)

check("ctrl+shift+space inside a tool commits it", function()
  local sid, w = new_session("again")
  local a = w.block_ids[1]
  local tool = open(sid, a, "rexkit_goto")
  local res = act(sid, tool, "rexkit_goto")
  eq(queued(res), "client.mode.exit_all session.select", "commit")
  wait_until("the float to close", function() return #floats(sid) == 0 end)
end)

check("after a reload (empty registry) a tool is found by its label and owner.json", function()
  local sid, w = new_session("recover")
  local a = w.block_ids[1]
  local diag = act(sid, a, "rexkit_diagnostics").result
  local binary = diag.settings.binary
  local dir = diag.state_dir .. "/tools/recover" .. os.time()
  assert(rex.exec({ "/bin/mkdir", "-p", dir }).ok, "mkdir")
  local layer = call("session.new_layer", { session_id = sid, bounds = { 0.1, 0.1, 0.8, 0.8 }, focus = true,
    layout = rex.layout.block { flavor = SHELL, label = "rexkit:goto", options = { shell = "none",
      command = { binary, "goto", "--session", sid, "--origin-block", a, "--origin-window", w.window_id, "--state", dir } } } })
  local tool = layer.block_ids[1]
  wait_until("the tool to start", function() return screen(sid, tool):find("ready", 1, true) end)
  local res = act(sid, tool, "rexkit_commit", { key = "enter" })
  eq(queued(res), "client.mode.exit_all session.select", "recovered and committed")
  wait_until("the float to close", function() return #floats(sid) == 0 end)
end)

check("a result can invoke an action about the origin pane", function()
  local sid, w = new_session("invoke")
  local a = w.block_ids[1]
  local tool = open(sid, a, "rexkit_keys", { mode = "pane" })
  eq(flag(tool_argv(), "--mode"), "pane", "keys --mode")
  type_into(sid, tool, "invoke rexkit_scratch")
  local res = act(sid, tool, "rexkit_commit", { key = "enter" })
  local selects = 0
  for _, q in ipairs(res.client_actions) do if q.action == "session.select" then selects = selects + 1 end end
  eq(selects, 2, "back to the origin, then Scratch")
  wait_until("Scratch", function() return #view(sid).windows == 2 end)
end)

check("tool flags and bounds: files, rename, scrollback", function()
  local sid, w = new_session("flags", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a = w.block_ids[1]
  local tool = open(sid, a, "rexkit_files")
  local argv = tool_argv()
  if argv then
    eq(argv[2], "files", "files")
    assert(flag(argv, "--shell") and flag(argv, "--directory"), "--shell/--directory")
    assert(rex.json.decode(flag(argv, "--editor-json"))[1], "--editor-json")
  end
  -- Every float covers the window (Rex's app draws only those whole); the
  -- tool draws in its card. The pane is 80 columns wide (no client owns it)
  -- -> the window ~160 -> the wide card.
  eq(table.concat(floats(sid)[1].layer.bounds, ","), "0,0,1,1", "files covers the window")
  if argv then
    eq(flag(argv, "--card"), "0.1100,0.1300,0.7800,0.7400", "wide card")
    eq(flag(argv, "--backdrop"), "clear", "the backdrop setting (default clear) goes with the card")
  end
  act(sid, tool, "rexkit_dismiss", { key = "escape" })
  wait_until("close", function() return #floats(sid) == 0 end)

  tool = open(sid, a, "rexkit_rename_pane")
  argv = tool_argv()
  if argv then
    eq(flag(argv, "--target"), "pane", "--target")
    eq(flag(argv, "--id"), a, "--id")
    eq(flag(argv, "--current"), "A", "--current")
  end
  eq(table.concat(floats(sid)[1].layer.bounds, ","), "0,0,1,1", "rename covers the window")
  if argv then
    eq(flag(argv, "--card"), "0.0000,0.0000,0.5000,1.0000", "dialog card is the origin pane")
    eq(flag(argv, "--card-cells"), "56x9", "dialog size")
    eq(flag(argv, "--backdrop"), "clear", "dialog backdrop")
  end
  act(sid, tool, "rexkit_dismiss", { key = "escape" })
  wait_until("close", function() return #floats(sid) == 0 end)

  local res
  tool, res = open(sid, a, "rexkit_scrollback", { start = "page-up" })
  eq(flag(tool_argv(), "--start"), "page-up", "--start")
  local layer = floats(sid)[1].layer
  eq(table.concat(layer.bounds, ","), "0,0,1,1", "scrollback covers the window")
  eq(flag(tool_argv(), "--card"), nil, "Neovim cannot draw a card")
  eq(flag(tool_argv(), "--backdrop"), nil, "no card, no backdrop")
  -- Neovim owns ⏎/⎋ there: no picker mode, and a stray dismiss is a miss.
  eq(queued(res), "client.mode.exit_all", "scrollback enters no picker mode")
  eq(queued(act(sid, tool, "rexkit_dismiss", { key = "escape" })), "client.mode.exit_all pane.send_key", "⎋ goes to Neovim")
  eq(#floats(sid), 1, "still open")
  call("session.close_layer", { session_id = sid, layer_id = layer.layer_id })
  wait_until("close", function() return #floats(sid) == 0 end)
end)

check("next agent: jumps to a blocked agent in another session", function()
  local sid, w = new_session("agent-here")
  local other, ow = new_session("agent-there", rex.layout.horizontal(0.5, cat("idle"), cat("agent")))
  local agent = ow.block_ids[2]
  pid(other, agent)
  type_into(other, agent, "\27]7501;state=blocked:kind=question\7\n")
  wait_until("the status", function()
    local st = rex.call(TERMINAL .. ".program_status", { session_id = other, block_id = agent, args = {} })
    return st and next(st.records or {}) ~= nil
  end)
  local res = act(sid, w.block_ids[1], "rexkit_next_agent")
  eq(res.result.block_id, agent, "found the agent")
  eq(queued_args(res, "session.select").session_id, other, "select its session")
end)

-- ⌃⇧Space in a tool other than Go to --------------------------------------------------------

-- has_queued reports whether a response queued the client action name.
local function has_queued(res, name) return queued_args(res, name) ~= nil end

check("ctrl+shift+space in Files closes it (no commit) and opens Go to", function()
  local sid, w = new_session("files-goto", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a = w.block_ids[1]
  call("session.focus_block", { session_id = sid, block_id = a })
  local files = open(sid, a, "rexkit_files")
  local res = act(sid, files, "rexkit_goto")
  assert(not has_queued(res, "session.select"), "Files committed: " .. queued(res))
  assert(not has_queued(res, "pane.send_key"), "the chord went to a pane")
  eq(res.result.tool, "goto", "Go to opened")
  eq(queued_args(res, "client.mode.enter").name, "picker", "picker mode again")
  local argv = wait_until("Go to to start", function()
    local last = tool_argv()
    return last and last[2] == "goto" and last
  end)
  eq(flag(argv, "--origin-block"), a, "Go to is about the origin pane, not the Files float")
  wait_until("Files to close", function()
    local f = floats(sid)
    return #f == 1 and f[1].block.label == "rexkit:goto" and f[1].block
  end)
  act(sid, res.result.block_id, "rexkit_dismiss", { key = "escape" })
  wait_until("Go to to close", function() return #floats(sid) == 0 end)
  eq(focused(sid), a, "focus back on the origin")
end)

check("ctrl+shift+space in Rename never saves; a zoomed origin stays zoomed", function()
  local sid, w = new_session("rename-goto", rex.layout.horizontal(0.5, cat("A"), cat("B")))
  local a = w.block_ids[1]
  call("session.zoom_block", { session_id = sid, block_id = a, zoom = true })
  local rename = open(sid, a, "rexkit_rename_pane")
  local res = act(sid, rename, "rexkit_goto")
  assert(not has_queued(res, "session.select"), "Rename committed: " .. queued(res))
  eq(res.result.tool, "goto", "Go to opened")
  local tool = res.result.block_id
  wait_until("the tool to start", function() return screen(sid, tool):find("ready", 1, true) end)
  eq(tool_argv()[2], "goto", "the last tool started")
  eq(has(tool_argv(), "--zoomed"), true, "Go to knows the origin was zoomed")
  act(sid, tool, "rexkit_dismiss", { key = "escape" })
  wait_until("Go to to close", function() return #floats(sid) == 0 end)
  local focus, zoom = focused(sid)
  eq(focus, a, "focus back on A")
  eq(zoom, a, "A zoomed again")
end)

check("ctrl+shift+space in a stale picker mode opens Go to; the pane gets nothing", function()
  local sid, w = new_session("stale-goto")
  local a = w.block_ids[1]
  local res = act(sid, a, "rexkit_goto") -- what picker/ctrl+shift+space runs
  assert(not has_queued(res, "pane.send_key"), "the chord went to the pane")
  eq(queued(res), "client.mode.exit_all client.mode.enter", "leaves the stale mode, enters a fresh one")
  eq(res.result.tool, "goto", "Go to opened")
  wait_until("the tool to start", function() return screen(sid, res.result.block_id):find("ready", 1, true) end)
  act(sid, res.result.block_id, "rexkit_dismiss", { key = "ctrl+c" })
  wait_until("close", function() return #floats(sid) == 0 end)
end)

check("a second ⎋ right after one that stays is not lost", function()
  if not (rex.args.rex and rex.args.server) then return end
  local sid, w = new_session("queued")
  local a = w.block_ids[1]
  local tool = open(sid, a, "rexkit_goto")
  type_into(sid, tool, "slowback") -- the first ⎋ only backs out (stay), after 300ms
  local req = rex.json.encode({ session_id = sid, block_id = tool, action = "rexkit_dismiss",
    args = { key = "escape" }, origin = "key" })
  local argv = { rex.args.rex, "--autostart=false", "-S", rex.args.server, "api", "call", "client.action", req }
  local one = rex.exec.start(argv)
  rex.sleep(0.1)
  local two = rex.exec.start(argv) -- while the first still waits for the tool
  local r1, r2 = one:wait(10), two:wait(10)
  assert(r1.ok and r2.ok, "dismiss failed: " .. tostring(r1.stderr) .. tostring(r2.stderr))
  -- Rex usually runs the second action only after the first returns; when a
  -- Rex call yields, it lands while the first waits (busy) and is replayed.
  -- Either way exactly one of them closes the tool.
  local _, closes = (r1.stdout .. r2.stdout):gsub('"action":"close"', "")
  eq(closes, 1, "close results")
  wait_until("the second ⎋ to close the tool", function() return #floats(sid) == 0 end)
  eq(focused(sid), a, "focus back")
end)

-- Tab history -------------------------------------------------------------------------------

-- fresh_core loads lua/core.lua into a new app table, as a config reload
-- would, with its state in state_dir.
local function fresh_core(state_dir)
  local diag = act(sessions[1], view(sessions[1]).windows[1].focused_block_id, "rexkit_diagnostics").result
  local app = { root = diag.root, home = os.getenv("HOME") or "/", warnings = {}, settings = diag.settings,
    paths = { state = state_dir, config = state_dir } }
  function app.warn(message) app.warnings[#app.warnings + 1] = message end
  dofile(diag.root .. "/lua/core.lua")(app)
  return app
end

check("tmux n records the last tab; it survives a reload", function()
  local sid, w = new_session("lasttab")
  local two = call("session.new_window", { session_id = sid, window_label = "two", focus = false, layout = cat("T2") })
  local a = w.block_ids[1]
  call("session.focus_block", { session_id = sid, block_id = a })
  local res = act(sid, a, binding("tmux", "n").action)
  eq(queued_args(res, "session.select").window_id, two.window_id, "tmux n")
  res = act(sid, two.block_ids[1], "rexkit_last_tab")
  eq(queued_args(res, "session.select").window_id, w.window_id, "last tab after tmux n")
  local diag = act(sid, a, "rexkit_diagnostics").result
  local reloaded = fresh_core(diag.state_dir)
  eq(reloaded.session_state(sid).last_tab, two.window_id, "last tab after a reload")
end)

-- State file ----------------------------------------------------------------------------------

local function scratch_dir(name)
  local diag = act(sessions[1], view(sessions[1]).windows[1].focused_block_id, "rexkit_diagnostics").result
  local dir = diag.state_dir .. "/check-" .. name .. "-" .. os.time()
  assert(rex.exec({ "/bin/rm", "-rf", dir }).ok and rex.exec({ "/bin/mkdir", "-p", dir }).ok, "mkdir " .. dir)
  return dir
end

local function files_in(dir)
  local out = rex.exec({ "/bin/ls", "-A", dir }).stdout
  local list = {}
  for name in out:gmatch("[^\n]+") do list[#list + 1] = name end
  return list
end

check("workspace.json of the wrong shape is repaired, not fatal", function()
  local dir = scratch_dir("shape")
  local bad = [[{"s1": 5, "s2": {"parked": 3, "scratch": 7, "previous": "window:1", "last_tab": []},
    "s3": {"parked": {"block:1": {"window": "window:2", "sibling": 4, "sibling_blocks": "x", "ratio": "half",
      "next_windows": ["window:3", 5]}, "block:2": 9, "block:3": {"ratio": 0.4, "sibling_blocks": ["block:9"]}}}}]]
  local file = assert(io.open(dir .. "/workspace.json", "w"))
  file:write(bad)
  file:close()
  local app = fresh_core(dir)
  assert(#app.warnings == 1, "one warning about the repair: " .. rex.json.encode(app.warnings))
  eq(next(app.session_state("s1").parked), nil, "s1 replaced by an empty state")
  local s2 = app.session_state("s2")
  eq(s2.scratch, nil, "a non-string scratch is dropped")
  eq(s2.previous, "window:1", "a good field is kept")
  eq(s2.last_tab, nil, "a non-string last_tab is dropped")
  eq(type(s2.parked), "table", "a non-table parked is replaced")
  local parked = app.session_state("s3").parked
  eq(parked["block:2"], nil, "a non-table record is dropped")
  local r = parked["block:1"]
  eq(r.window, "window:2", "the record's good field is kept")
  eq(r.sibling, nil, "a non-string sibling is dropped")
  eq(r.sibling_blocks, nil, "a non-list sibling_blocks is dropped")
  eq(r.ratio, nil, "a non-number ratio is dropped")
  eq(r.next_windows, nil, "a list with a non-string is dropped")
  eq(parked["block:3"].ratio, 0.4, "a good ratio is kept")
  eq(parked["block:3"].sibling_blocks[1], "block:9", "a good list is kept")
  app.save_state() -- the repaired state is written back (the sessions are gone, so it's empty)
  local saved = rex.json.decode(assert(io.open(dir .. "/workspace.json")):read("*a"))
  eq(next(saved), nil, "saved without the dead sessions")
  -- a clean file loads without a warning
  eq(#fresh_core(dir).warnings, 0, "no warning for a good file")
  rex.exec({ "/bin/rm", "-rf", dir })
end)

check("write_file fails cleanly and leaves no temporary file", function()
  local dir = scratch_dir("write")
  local app = fresh_core(dir)
  app.write_file(dir .. "/ok.json", "{}")
  eq(assert(io.open(dir .. "/ok.json")):read("*a"), "{}", "written")
  assert(rex.exec({ "/bin/mkdir", "-p", dir .. "/taken/child" }).ok, "mkdir")
  local ok, err = pcall(app.write_file, dir .. "/taken", "{}") -- rename onto a non-empty directory fails
  assert(not ok and tostring(err):find("cannot write", 1, true), "error: " .. tostring(err))
  ok, err = pcall(app.write_file, dir .. "/missing/x.json", "{}")
  assert(not ok and tostring(err):find("cannot write", 1, true), "error: " .. tostring(err))
  local names = table.concat(files_in(dir), " ")
  eq(names, "ok.json taken", "files left")
  rex.exec({ "/bin/rm", "-rf", dir })
end)

-- Toast ---------------------------------------------------------------------------------------

check("the toast strip is four or five rows of a 70- and a 24-row window", function()
  local diag = act(sessions[1], view(sessions[1]).windows[1].focused_block_id, "rexkit_diagnostics").result
  -- tools.lua with a stub app: it registers nothing, only defines functions.
  local app = { settings = { commit_via = "global" }, paths = { state = diag.state_dir } }
  function app.action() end
  function app.bind() end
  dofile(diag.root .. "/lua/tools.lua")(app)
  for _, rows in ipairs({ 70, 24 }) do
    local cells = app.toast_height(rows) * rows
    assert(cells >= 4 and cells < 6, rows .. " rows: a strip of " .. cells .. " rows (the toast needs 4: air and its card)")
  end
end)

for _, sid in ipairs(sessions) do rex.call("session.destroy", { session_id = sid }) end

return { ok = next(failed) == nil, passed = passed, failed = failed }
