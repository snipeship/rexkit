-- Shared helpers: Rex calls, actions and bindings, layout lookups, focus,
-- and small file utilities.
return function(app)
  local TERMINAL = "com.superlogical.terminal"
  app.TERMINAL = TERMINAL
  app.SHELL = TERMINAL .. ".shell"
  app.actions = {} -- action name -> run function, so actions can call each other
  app.keys = {} -- action name -> chords bound to it ("ctrl+p e"), for hints
  app.redirect = {} -- closing tool block -> origin block, while a tool's result runs

  -- The keyboard modes, entered from normal mode with their prefix: the
  -- Zellij-style ones (zellij_modes), the tmux-style prefix (tmux_prefix),
  -- and locked, which is there while either is (it locks their keys).
  local MODES = {
    { name = "pane", key = "ctrl+p", title = "Pane mode", style = "zellij" },
    { name = "tab", key = "ctrl+t", title = "Tab mode", style = "zellij" },
    { name = "resize", key = "ctrl+n", title = "Resize mode", style = "zellij" },
    { name = "move", key = "ctrl+h", title = "Move mode", style = "zellij" },
    { name = "scroll", key = "ctrl+s", title = "Scroll mode", style = "zellij" },
    { name = "session", key = "ctrl+o", title = "Session mode", style = "zellij" },
    { name = "tmux", key = "ctrl+b", title = "Tmux prefix", once = true, style = "tmux" },
    { name = "locked", key = "ctrl+g", title = "Lock keys", passthrough = true, style = "locked" },
  }
  local on = { zellij = app.settings.zellij_modes, tmux = app.settings.tmux_prefix }
  on.locked = on.zellij or on.tmux
  app.MODES, app.prefix = {}, {} -- the modes that are on, and their prefixes
  app.modes_off = {} -- mode name -> true for a mode its setting turned off
  for _, mode in ipairs(MODES) do
    if on[mode.style] then
      app.MODES[#app.MODES + 1] = mode
      app.prefix[mode.name] = mode.key
    else
      app.modes_off[mode.name] = true
    end
  end

  -- Key lists (modes.lua). Persistent modes are not exclusive, so global
  -- shortcuts and every ⌘ key keep working in them; instead each mode blocks
  -- these keys unless it or the root binds them, so typing can't leak into
  -- the terminal. ⏎ ⎋ ⌃C are left out: every mode binds them to leave. ⌘ is
  -- never blocked (the app's own shortcuts).
  local LETTERS, DIGITS = {}, {}
  for c = 97, 122 do LETTERS[#LETTERS + 1] = string.char(c) end
  for c = 48, 57 do DIGITS[#DIGITS + 1] = string.char(c) end
  local PUNCTUATION = { "`", "-", "=", "[", "]", "\\", ";", "'", ",", ".", "/" }
  local EDITING = { "space", "tab", "backspace" }
  local MOTION = { "delete", "left", "right", "up", "down", "home", "end", "pageup", "pagedown" }

  local function combos(mods, groups)
    local keys = {}
    for _, mod in ipairs(mods) do
      for _, group in ipairs(groups) do
        for _, base in ipairs(group) do keys[#keys + 1] = mod .. base end
      end
    end
    return keys
  end

  app.BLOCK_KEYS = combos({ "", "shift+", "ctrl+", "alt+", "alt+shift+" },
    { LETTERS, DIGITS, PUNCTUATION, EDITING, MOTION })
  -- The Tmux prefix leaves after one key, as tmux does: every one of these
  -- that it doesn't bind still counts as its key (consumed, then back to normal).
  app.TMUX_KEYS = combos({ "", "shift+" }, { LETTERS, DIGITS, PUNCTUATION, EDITING })
  for _, key in ipairs(combos({ "ctrl+" }, { LETTERS })) do app.TMUX_KEYS[#app.TMUX_KEYS + 1] = key end

  math.randomseed(os.time())

  -- Errors ------------------------------------------------------------------

  -- Rex reports `stream response "control-N" failed: <message> (status=N code=C)`.
  function app.clean(err)
    local text = tostring(err)
    return text:match("failed: (.-) %(status=%d+") or text
  end

  function app.fail(message) error("rexkit: " .. message, 0) end

  -- call runs a server method and raises a clean error when it fails.
  function app.call(method, args)
    local result, err = rex.call(method, args or {})
    if err then app.fail(app.clean(err)) end
    return result or {}
  end

  -- terminal calls a terminal block method; like rex.call it never raises.
  function app.terminal(sid, block, method, args)
    return rex.call(TERMINAL .. "." .. method, { session_id = sid, block_id = block, args = args or {} })
  end

  -- Actions, bindings and client actions -------------------------------------

  -- has_ui is true when a client ran the action from a key or the palette:
  -- only then can it be asked to perform client actions.
  function app.has_ui(ctx)
    return ctx.client_id ~= nil and (ctx.origin == "key" or ctx.origin == "palette")
  end

  function app.queue(ctx, action, args)
    if app.has_ui(ctx) then rex.client.queue(action, args) end
  end

  function app.exit_modes(ctx) app.queue(ctx, "client.mode.exit_all") end

  -- action defines rexkit_<spec.name>. spec.exits leaves every mode first
  -- (commands that open something or change the layout); spec.internal keeps
  -- plumbing out of the palette's "rexkit" category.
  function app.action(spec)
    local name = "rexkit_" .. spec.name
    local title, description = spec.title, spec.long
    if spec.long and not app.settings.compact_labels then title, description = spec.long, nil end
    local function run(ctx, args)
      if spec.exits then app.exit_modes(ctx) end
      return spec.run(ctx, args or {})
    end
    rex.action {
      name = name,
      title = title,
      description = description,
      keywords = spec.keywords or spec.long,
      category = spec.internal and "rexkit internal" or "rexkit",
      args = spec.args,
      repeats = spec.repeats,
      run = run,
    }
    app.actions[name] = run
    return name
  end

  -- command wraps a native client action so the mode is left before it runs
  -- (a new pane or a dialog must receive typing; a mode blocks text keys).
  function app.command(name, title, action, args)
    return app.action { name = name, title = title, exits = true, run = function(ctx)
      app.queue(ctx, action, args)
      return { queued = action }
    end }
  end

  app.global = {} -- key -> {action, args} of every global binding (app.replay)

  -- bind binds key in mode (nil: globally). A binding in a mode its setting
  -- turned off is left out, so bindings.lua can list every mode's keys.
  function app.bind(mode, key, action, args)
    if mode and app.modes_off[mode] then return end
    rex.bind(mode and (mode .. "/" .. key) or key, action, args)
    if not mode then app.global[key] = { action = action, args = args } end
    local chord = mode and app.prefix[mode] and (app.prefix[mode] .. " " .. key) or key
    local chords = app.keys[action] or {}
    chords[#chords + 1] = chord
    app.keys[action] = chords
  end

  -- replay does what key does outside every mode: its global binding (a
  -- rexkit action runs here, a native one is queued), else the pane gets
  -- the key. For a mode that outlived its tool (hints).
  function app.replay(ctx, key)
    local binding = app.global[key]
    if not binding then
      app.queue(ctx, "pane.send_key", { key = key })
      return { sent = key }
    end
    local run = app.actions[binding.action]
    if run then return run(ctx, binding.args or {}) end
    app.queue(ctx, binding.action, binding.args)
    return { queued = binding.action }
  end

  -- chord is the first key that runs action, such as "ctrl+p e".
  function app.chord(action)
    local chords = app.keys[action]
    return chords and chords[1]
  end

  -- Views and layouts ---------------------------------------------------------

  function app.view(sid) return app.call("session.view", { session_id = sid }) end

  local function walk(node, parents, fn)
    if not node then return end
    if node.block_id then return fn(node, parents) end
    local split = node.split
    if not split then return end
    for _, side in ipairs({ "before", "after" }) do
      local path = {}
      for i, parent in ipairs(parents) do path[i] = parent end
      path[#path + 1] = { split = split, side = side }
      walk(split[side], path, fn)
    end
  end

  -- blocks lists every placed block as {id, label, rect, window, layer,
  -- parents}; parents runs from the outermost split to the innermost.
  function app.blocks(view)
    local list = {}
    for _, window in ipairs(view.windows or {}) do
      for _, layer in ipairs(window.layers or {}) do
        local labels = {}
        for _, block in ipairs(layer.blocks or {}) do labels[block.block_id] = block.label end
        walk(layer.layout, {}, function(node, parents)
          list[#list + 1] = { id = node.block_id, label = labels[node.block_id], rect = node.rect,
            window = window, layer = layer, parents = parents }
        end)
      end
    end
    return list
  end

  function app.find_block(view, id)
    if not id then return nil end
    for _, item in ipairs(app.blocks(view)) do
      if item.id == id then return item end
    end
  end

  function app.find_window(view, id)
    if not id then return nil end
    for i, window in ipairs(view.windows or {}) do
      if window.window_id == id then return window, i end
    end
  end

  -- tiled_blocks lists a window's blocks in its tiled layer.
  function app.tiled_blocks(view, window_id)
    local list = {}
    for _, item in ipairs(app.blocks(view)) do
      if item.window.window_id == window_id and item.layer.kind == "tiled" then list[#list + 1] = item end
    end
    return list
  end

  function app.first_block(node)
    if not node then return nil end
    if node.block_id then return node.block_id end
    return app.first_block(node.split.before) or app.first_block(node.split.after)
  end

  function app.node_id(node) return node.block_id or node.split.split_id end

  function app.leaves(node)
    local ids = {}
    walk(node, {}, function(leaf) ids[#ids + 1] = leaf.block_id end)
    return ids
  end

  -- contains reports whether a block or split id is anywhere in window.
  function app.contains(window, id)
    local function find(node)
      if not node then return false end
      if app.node_id(node) == id then return true end
      return node.split ~= nil and (find(node.split.before) or find(node.split.after))
    end
    for _, layer in ipairs(window.layers or {}) do
      if find(layer.layout) then return true end
    end
    return false
  end

  -- current returns the view and the block the action is about: ctx.block_id,
  -- or the focused block when that is gone (e.g. a tool that just closed).
  function app.current(ctx)
    if not ctx.session_id then app.fail("run this from a pane") end
    local view = app.view(ctx.session_id)
    local item = app.find_block(view, app.redirect[ctx.block_id] or ctx.block_id)
    if not item then
      local window = app.find_window(view, view.active_window_id)
      item = window and app.find_block(view, window.focused_block_id)
    end
    if not item then app.fail("focus a pane first") end
    return view, item
  end

  -- select focuses a window (or a block in it) and shows it in the client.
  function app.select(ctx, window_id, block_id)
    if block_id then
      app.call("session.focus_block", { session_id = ctx.session_id, block_id = block_id })
    else
      app.call("session.focus_window", { session_id = ctx.session_id, window_id = window_id })
    end
    app.queue(ctx, "session.select", { session_id = ctx.session_id, window_id = window_id })
  end

  -- Terminals -----------------------------------------------------------------

  local function process(sid, block)
    local info = app.terminal(sid, block, "process")
    return info and (info.foreground or info.child), info
  end

  function app.cwd(sid, block)
    local _, info = process(sid, block)
    local cwd = info and ((info.foreground and info.foreground.cwd) or (info.child and info.child.cwd))
    return cwd or app.home
  end

  -- name describes a block to the user: its label, else its program.
  function app.name(sid, item)
    if item.label and item.label ~= "" and item.label ~= "shell" then return item.label end
    local proc = process(sid, item.id)
    return proc and proc.name or "pane"
  end

  function app.shell_layout(cwd, label)
    return rex.layout.block { flavor = app.SHELL, label = label,
      options = { shell = app.settings.shell, cwd = cwd } }
  end

  -- slot_layout is a short-lived block that only holds a place while another
  -- block is swapped into it; it is closed right after.
  function app.slot_layout()
    return rex.layout.block { flavor = app.SHELL, label = "rexkit:slot",
      options = { shell = "none", command = { "/bin/cat" }, cwd = app.home } }
  end

  function app.close_block(sid, block)
    return rex.call("block.close", { session_id = sid, block_id = block })
  end

  -- move_to_new_tab puts an existing block (its program keeps running) into a
  -- new tab and returns the tab's window id.
  function app.move_to_new_tab(sid, block, label)
    local created = app.call("session.new_window", { session_id = sid, window_label = label, focus = false,
      layout = app.slot_layout() })
    local slot = created.block_ids[1]
    app.call("session.swap_blocks", { session_id = sid, block_id = block, other_block_id = slot })
    app.close_block(sid, slot)
    return created.window_id
  end

  -- Files ---------------------------------------------------------------------

  local made = {}
  function app.ensure_dir(path)
    if made[path] then return end
    local result, err = rex.exec({ "/bin/mkdir", "-p", "-m", "700", path })
    if not (result and result.ok) then
      app.fail("cannot create " .. path .. ": " .. tostring(result and result.stderr or err))
    end
    made[path] = true
  end

  function app.exists(path)
    local file = io.open(path, "r")
    if not file then return false end
    file:close()
    return true
  end

  function app.read_json(path)
    local file = io.open(path, "r")
    if not file then return nil end
    local text = file:read("*a")
    file:close()
    local ok, value = pcall(rex.json.decode, text)
    if ok and type(value) == "table" then return value end
  end

  -- write_file replaces path atomically; the temporary name is unique so
  -- concurrent writers (a reload, a test script) never share it. A failed
  -- write or close (a full disk) never replaces the previous file.
  function app.write_file(path, text)
    local tmp = ("%s.%d-%d.tmp"):format(path, os.time(), math.random(1, 2 ^ 30))
    local file, err = io.open(tmp, "w")
    if not file then app.fail("cannot write " .. path .. ": " .. tostring(err)) end
    local wrote, werr = file:write(text)
    local closed, cerr = file:close()
    if not wrote or not closed then
      os.remove(tmp)
      app.fail("cannot write " .. path .. ": " .. tostring(werr or cerr))
    end
    local ok, why = os.rename(tmp, path)
    if not ok then
      os.remove(tmp)
      app.fail("cannot write " .. path .. ": " .. tostring(why))
    end
  end

  -- Per-session state ------------------------------------------------------------
  --
  -- workspace.json keeps, per session id, what must survive a config reload:
  --   scratch   window id of the Scratch tab              (workspace.lua)
  --   previous  tab to return to from Scratch             (workspace.lua)
  --   parked    block id -> return address of a parked pane (workspace.lua)
  --   last_tab  window shown before the latest tab switch (layout.lua)
  -- The file is written only by rexkit and `rexkit install`, but it is read
  -- defensively: an entry of the wrong shape is dropped or repaired (and the
  -- repair saved) instead of breaking every Scratch command for that session.

  local function is_list_of_strings(value)
    if type(value) ~= "table" then return false end
    for k, v in pairs(value) do
      if type(k) ~= "number" or type(v) ~= "string" then return false end
    end
    return true
  end

  local RECORD_FIELDS = { window = "string", label = "string", name = "string", sibling = "string",
    placeholder = "string", direction = "string", side = "string", ratio = "number",
    sibling_blocks = "strings", next_windows = "strings" }

  -- clean_record drops the fields of a parked pane's return address that have
  -- the wrong type (put_back has a fallback for each); true if it dropped any.
  local function clean_record(record)
    local changed = false
    for field, kind in pairs(RECORD_FIELDS) do
      local value = record[field]
      local ok = value == nil or (kind == "strings" and is_list_of_strings(value)) or type(value) == kind
      if field == "ratio" and ok and value ~= nil then ok = value > 0 and value < 1 end -- false for NaN too
      if not ok then record[field], changed = nil, true end
    end
    return changed
  end

  -- clean_state returns state with every entry of the wrong shape dropped or
  -- repaired, and whether it changed anything.
  function app.clean_state(state)
    local changed = false
    if type(state) ~= "table" then return {}, state ~= nil end
    for sid, st in pairs(state) do
      if type(sid) ~= "string" or type(st) ~= "table" then
        state[sid], changed = nil, true
      else
        for _, field in ipairs({ "scratch", "previous", "last_tab" }) do
          if st[field] ~= nil and type(st[field]) ~= "string" then st[field], changed = nil, true end
        end
        if st.parked ~= nil and type(st.parked) ~= "table" then st.parked, changed = nil, true end
        for id, record in pairs(st.parked or {}) do
          if type(id) ~= "string" or type(record) ~= "table" then
            st.parked[id], changed = nil, true
          elseif clean_record(record) then
            changed = true
          end
        end
      end
    end
    return state, changed
  end

  local state_path = app.paths.state .. "/workspace.json"
  local state, repaired = app.clean_state(app.read_json(state_path) or {})
  if repaired then app.warn("workspace.json had entries of the wrong shape; they were dropped") end
  -- saved is what the file holds; a repaired state differs, so it is written back.
  local saved = repaired and "" or rex.json.encode(state)
  local pruned = false

  -- session_state returns the state of one session, created on first use.
  function app.session_state(sid)
    local st = state[sid] or {}
    st.parked = st.parked or {}
    state[sid] = st
    return st
  end

  -- save_state writes the state when it changed. The first save after a load
  -- also forgets sessions that no longer exist.
  function app.save_state()
    if not pruned then
      local list = rex.call("session.list", {})
      if list and list.sessions then
        local alive = {}
        for _, s in ipairs(list.sessions) do alive[s.session_id] = true end
        for sid in pairs(state) do
          if not alive[sid] then state[sid] = nil end
        end
        pruned = true
      end
    end
    local text = rex.json.encode(state)
    if text == saved then return end
    app.ensure_dir(app.paths.state)
    app.write_file(state_path, text)
    saved = text
  end
end
