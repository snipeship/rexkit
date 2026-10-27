-- Floating tools: open `rexkit <tool>` in a floating layer over the current
-- tab, then carry ⏎ / ⎋ / ⌃C to it through the "picker" mode so
-- that its result can switch the client to another session, which only a
-- key-run action can do. Normal panes never see this layer: the mode exists
-- only while a tool is open.
return function(app)
  local pickers = {} -- tool block id -> {tool, session_id, block_id, origin_block, zoomed, dir, busy, queued}
  local tools_dir = app.paths.state .. "/tools"
  local KEY_BYTES = { enter = "\r", escape = "\27", ["ctrl+c"] = "\3" }
  local DIALOG_COLS, DIALOG_ROWS = 56, 9
  local TOAST_ROWS = 4 -- a row of air, then the toast's card (rexkit toast)
  -- Tools that read ⏎ and ⎋ themselves (Neovim, an editor): no picker mode, no
  -- handshake. Their state dirs are still tracked, so they are GC'd.
  local OWN_KEYS = { scrollback = true, settings = true }

  local function binary()
    if not app.exists(app.settings.binary) then
      app.fail(app.settings.binary .. " is missing; reinstall rexkit (from a clone: go run ./cmd/rexkit install in " .. app.root .. ")")
    end
    return app.settings.binary
  end

  local function new_state_dir()
    app.ensure_dir(tools_dir)
    local result, err = rex.exec({ "/usr/bin/mktemp", "-d", tools_dir .. "/XXXXXXXX" })
    if not (result and result.ok) then app.fail("cannot create a tool folder: " .. tostring(result and result.stderr or err)) end
    return (result.stdout:gsub("%s+$", ""))
  end

  local function remove_state_dir(dir)
    for _, name in ipairs({ "result.json", "owner.json", "commit" }) do os.remove(dir .. "/" .. name) end
    os.remove(dir) -- only succeeds once the tool removed its own files
  end

  -- Bounds -------------------------------------------------------------------

  -- Rex's app draws a floating layer at [x, y, w, h] as if w and h were its
  -- right and bottom edges, while the server sizes the layer's terminal from
  -- the documented [x, y, w, h]: only a float at
  -- the window's top-left corner is drawn whole. So every float covers the
  -- whole window, and a rexkit tool gets the card it draws in as flags
  -- (popup.Card); the rest of its float stays see-through.
  local WHOLE = { 0, 0, 1, 1 }

  -- window_cells estimates the window's size in cells from a pane in it.
  local function window_cells(sid, item, rect)
    local size = app.terminal(sid, item.id, "size")
    if not (size and size.columns and size.rows) then return 120, 40 end
    return size.columns / rect[3], size.rows / rect[4]
  end

  local function area(r)
    return string.format("%.4f,%.4f,%.4f,%.4f", r[1], r[2], r[3], r[4])
  end

  -- card_flags place a tool for size "float" (centred, sized by the window's
  -- width), "dialog" (DIALOG_COLS x DIALOG_ROWS centred over the pane at
  -- rect) or "whole" (the whole window, for tools that cannot draw a card).
  -- A card comes with the backdrop setting: what shows around it.
  local function card_flags(size, rect, cols)
    if size == "dialog" then
      return { "--card", area(rect), "--card-cells", DIALOG_COLS .. "x" .. DIALOG_ROWS,
        "--backdrop", app.settings.backdrop }
    end
    local w, h
    if size == "whole" or cols < 80 then
      return {}
    elseif cols >= 120 then
      w, h = 0.78, 0.74
    else
      w, h = 0.94, 0.88
    end
    return { "--card", area({ (1 - w) / 2, (1 - h) / 2, w, h }), "--backdrop", app.settings.backdrop }
  end

  local function terminal_layout(label, command, cwd, exit)
    return rex.layout.block { flavor = app.SHELL, label = label,
      options = { shell = "none", login = false, command = command, cwd = cwd or app.home,
        exit = exit or { on_completion = true } } }
  end

  -- Launching ------------------------------------------------------------------

  -- open_float opens spec.command in a new focused floating layer over the
  -- whole of the origin's window. spec: {label, command, cwd}.
  local function open_float(ctx, origin, spec)
    local created = app.call("session.new_layer", { session_id = ctx.session_id, window_id = origin.window.window_id,
      bounds = WHOLE, focus = true, layout = terminal_layout(spec.label, spec.command, spec.cwd) })
    return { block_id = created.block_ids[1], layer_id = created.layer_id }
  end

  -- toast_height is the height of the toast's strip as a fraction of a
  -- window rows tall: TOAST_ROWS, rounded either way.
  function app.toast_height(rows)
    return math.floor(math.min(1, (TOAST_ROWS + 0.5) / rows) * 10000) / 10000
  end

  -- notify shows a toast in the window's top-right corner without taking
  -- focus: a strip of TOAST_ROWS along the top of the window, in whose corner
  -- the toast draws its rounded card. Best effort: a missing or failing toast closes at once.
  function app.notify(ctx, message, kind)
    if not (ctx.session_id and app.exists(app.settings.binary)) then return end
    local view = rex.call("session.view", { session_id = ctx.session_id })
    local window = view and app.find_window(view, view.active_window_id)
    local focused = window and app.find_block(view, window.focused_block_id)
    if not focused then return end
    local _, rows = window_cells(ctx.session_id, focused, focused.rect)
    rex.call("session.new_layer", { session_id = ctx.session_id, window_id = window.window_id,
      bounds = { 0, 0, 1, app.toast_height(rows) }, focus = false,
      layout = terminal_layout("rexkit:toast", { app.settings.binary, "toast", "--message", message,
        "--kind", kind or "info" }, nil, { on_completion = true, quick_exit_threshold_ms = 0 }) })
  end

  local function alive(entry)
    local view = rex.call("session.view", { session_id = entry.session_id })
    return view ~= nil and app.find_block(view, entry.block_id) ~= nil
  end

  local function forget(entry)
    pickers[entry.block_id] = nil
    remove_state_dir(entry.dir)
  end

  -- prune forgets tools that closed without a handshake (they lost focus, or
  -- handled a key themselves while the picker mode was not active).
  local function prune()
    for _, entry in pairs(pickers) do
      if not entry.busy and not alive(entry) then forget(entry) end
    end
  end

  -- open_tool runs `rexkit <tool> <shared flags> <flags>` in a float and,
  -- unless commit_via is "global", enters the picker mode. opts.zoomed says
  -- the origin counts as zoomed even though it isn't right now (a tool that
  -- replaces another one, whose float ended the zoom).
  function app.open_tool(ctx, tool, opts)
    opts = opts or {}
    local _, origin = app.current(ctx)
    if (origin.label or ""):match("^rexkit:") and origin.layer.kind == "floating" then
      app.fail("close the open tool first")
    end
    app.exit_modes(ctx) -- a mode would block the tool's typing
    prune()
    local bin = binary()
    local dir = new_state_dir()
    local window = origin.window
    local zoomed = opts.zoomed or window.zoomed_block_id == origin.id
    local argv = { bin, tool, "--session", ctx.session_id, "--origin-block", origin.id,
      "--origin-window", window.window_id, "--state", dir }
    if zoomed then argv[#argv + 1] = "--zoomed" end
    if ctx.client_id then
      argv[#argv + 1] = "--client"
      argv[#argv + 1] = ctx.client_id
    end
    if not app.settings.dismiss_on_blur then argv[#argv + 1] = "--dismiss-on-blur=false" end
    local rect = zoomed and WHOLE or origin.rect
    local cols = window_cells(ctx.session_id, origin, rect)
    for _, flag in ipairs(card_flags(opts.size or "float", rect, cols)) do argv[#argv + 1] = flag end
    for _, flag in ipairs(opts.flags or {}) do argv[#argv + 1] = flag end
    local ok, opened = pcall(open_float, ctx, origin, { label = "rexkit:" .. tool, command = argv, cwd = opts.cwd })
    if not ok then
      remove_state_dir(dir)
      error(opened, 0)
    end
    pickers[opened.block_id] = { tool = tool, session_id = ctx.session_id, block_id = opened.block_id,
      origin_block = origin.id, zoomed = zoomed, dir = dir }
    if not OWN_KEYS[tool] and app.settings.commit_via == "mode" then app.queue(ctx, "client.mode.enter", { name = "picker" }) end
    return { tool = tool, block_id = opened.block_id, layer_id = opened.layer_id, state = dir }
  end

  -- Handshake ------------------------------------------------------------------

  -- lookup finds the tool entry for a block. After a config reload the
  -- registry is empty: a float labelled rexkit:<tool> is still ours, and its
  -- state dir is the one whose owner.json names the block (popup.Claim).
  local function lookup(ctx, recover)
    local entry = pickers[ctx.block_id]
    if entry and OWN_KEYS[entry.tool] then return nil end
    if entry or not recover or not ctx.session_id then return entry end
    local view = rex.call("session.view", { session_id = ctx.session_id })
    local item = view and app.find_block(view, ctx.block_id)
    local tool = item and item.layer.kind == "floating" and (item.label or ""):match("^rexkit:(.+)$")
    if not tool or OWN_KEYS[tool] then return nil end
    local found = rex.exec({ "/usr/bin/grep", "-rlF", "--include=owner.json", ctx.block_id, tools_dir })
    local owner = found and found.ok and found.stdout:match("[^\n]+")
    if not owner then return nil end
    entry = { tool = tool, session_id = ctx.session_id, block_id = ctx.block_id, dir = owner:match("^(.*)/[^/]+$") }
    pickers[ctx.block_id] = entry
    return entry
  end

  -- miss: the key was meant for whatever has focus, not a tool (e.g. the user
  -- clicked into another pane). Send it on and leave the stale mode.
  local function miss(ctx, key)
    app.queue(ctx, "client.mode.exit_all")
    app.queue(ctx, "pane.send_key", { key = key })
    return { picker = false }
  end

  local function invoke(ctx, entry, result)
    if result.invoke:find(".", 1, true) then -- a client action, e.g. pane.split
      app.queue(ctx, result.invoke, result.args)
      return
    end
    -- Our own actions run in-process: rex.invoke drops the client actions an
    -- invoked action queues. Either way the action is about the origin pane,
    -- not the closing tool.
    app.redirect[entry.block_id] = entry.origin_block
    local ok, err = pcall(function()
      local run = app.actions[result.invoke]
      if run then return run(ctx, result.args or {}) end
      local _, failure = rex.invoke(result.invoke, result.args or {})
      if failure then app.fail(app.clean(failure)) end
    end)
    app.redirect[entry.block_id] = nil
    if not ok then error(err, 0) end
  end

  -- apply carries out a tool's result (popup/result.go has the vocabulary).
  local function apply(ctx, entry, result)
    if result.action == "stay" then return result end
    forget(entry)
    app.exit_modes(ctx)
    if result.action ~= "close" and result.session_id and result.session_id ~= "" then
      if result.block_id == entry.origin_block and entry.zoomed then
        -- back to a zoomed origin: zooming it also focuses it (focus alone would
        -- leave it unzoomed, and the closing tool then no longer has focus to re-zoom)
        rex.call("session.zoom_block", { session_id = result.session_id, block_id = result.block_id, zoom = true })
      elseif result.block_id then
        rex.call("session.focus_block", { session_id = result.session_id, block_id = result.block_id })
      end
      app.queue(ctx, "session.select", { session_id = result.session_id, window_id = result.window_id })
    end
    if result.invoke then invoke(ctx, entry, result) end
    return result
  end

  -- handshake writes key bytes into the tool and waits for its result. pty
  -- order guarantees the tool reads them after everything typed before.
  -- There is one result slot, so a key pressed while a handshake waits is
  -- never written at once: the latest one is kept and replayed only if the
  -- tool stayed open (⎋⎋ to clear a query and then close). After a jump or a
  -- close it is dropped, never sent on to the origin pane.
  --
  -- A ⏎ goes with the one-shot commit marker <state>/commit (popup.CommitFile),
  -- created (atomically) right before the byte is written and removed after
  -- the await, whatever happened: the tool takes it with the ⏎ it reads, and
  -- so tells this ⏎, whose result is read here, from one typed while the
  -- picker mode was not active (after a reload), whose result nobody reads.
  --
  -- The await uses `rexkit await`'s own timeout (popup.AwaitTimeout, 1.5 s).
  -- A tool that holds a ⏎ back for its data answers "stay" within
  -- popup.HandshakeGrace (1 s); like every "stay" that keeps the tool and the
  -- picker mode, without an error.
  local function handshake(ctx, entry, bytes)
    if entry.busy then
      entry.queued = bytes
      return { busy = true, queued = true }
    end
    entry.busy = true
    local result
    repeat
      local next_bytes = bytes
      bytes = nil
      os.remove(entry.dir .. "/result.json")
      local marker = next_bytes == KEY_BYTES.enter and entry.dir .. "/commit" or nil
      local ok, r = pcall(function()
        if marker then app.write_file(marker, "") end
        local _, err = app.terminal(entry.session_id, entry.block_id, "write", { data = next_bytes })
        if err then return { action = "close", reason = "the tool is gone" } end
        local done, why = rex.exec({ binary(), "await", "--state", entry.dir }, { timeout = 5 })
        if done and done.ok then return rex.json.decode(done.stdout) end
        if not alive(entry) then return { action = "close", reason = "the tool closed without a result" } end
        app.fail(entry.tool .. " did not answer: " .. tostring(done and done.stderr or why))
      end)
      if marker then os.remove(marker) end -- taken by the tool, or left by a failure or timeout
      if not ok then
        entry.busy, entry.queued = false, nil
        error(r, 0)
      end
      result = r
      if result.action == "stay" then bytes = entry.queued end
      entry.queued = nil
    until not bytes
    entry.busy = false
    return apply(ctx, entry, result)
  end

  local function commit(ctx, args)
    local entry = lookup(ctx, not args.global)
    if not entry then return miss(ctx, args.key or "enter") end
    return handshake(ctx, entry, KEY_BYTES.enter)
  end

  local function dismiss(ctx, args)
    local key = args.key or "escape"
    local entry = lookup(ctx, true)
    if not entry then return miss(ctx, key) end
    return handshake(ctx, entry, KEY_BYTES[key] or KEY_BYTES.escape)
  end

  app.action { name = "commit", title = "Confirm in tool", internal = true,
    args = { key = "string?", global = "boolean?" }, run = commit }
  app.action { name = "dismiss", title = "Back / close tool", internal = true,
    args = { key = "string?" }, run = dismiss }

  if app.settings.commit_via == "global" then
    app.bind(nil, "enter", "rexkit_commit", { key = "enter", global = true })
  else -- the picker mode: only these keys go through Lua, and only while a tool is open
    rex.mode("picker", { exclusive = false })
    app.bind("picker", "enter", "rexkit_commit", { key = "enter" })
    app.bind("picker", "ctrl+shift+space", "rexkit_goto") -- see rexkit_goto: works in a stale mode too
    app.bind("picker", "escape", "rexkit_dismiss", { key = "escape" })
    app.bind("picker", "ctrl+c", "rexkit_dismiss", { key = "ctrl+c" })
  end

  -- Tools ------------------------------------------------------------------------

  -- gone waits (up to about a second) until a closing tool's block is gone,
  -- so that the focused block is the one the tool restored.
  local function gone(entry)
    for _ = 1, 25 do
      if not alive(entry) then return true end
      rex.exec({ "/bin/sleep", "0.04" })
    end
    return not alive(entry)
  end

  -- Go to (⌃⇧Space). Pressed again in Go to it confirms (jumps). Pressed in
  -- another tool it closes that tool without committing anything (⌃C always
  -- answers "close"; ⎋ may only back out) and opens Go to in its place. In a
  -- stale picker mode (no tool open) it just opens Go to.
  app.action { name = "goto", title = "Go to…", long = "Go to a session, tab, pane or agent",
    keywords = "switch jump find session tab pane agent", run = function(ctx)
      local entry = lookup(ctx, true)
      if not entry then return app.open_tool(ctx, "goto") end -- open_tool leaves any mode first
      if entry.tool == "goto" then return commit(ctx, { key = "ctrl+shift+space" }) end
      local closed = handshake(ctx, entry, KEY_BYTES["ctrl+c"])
      if closed.busy or closed.action == "stay" then return closed end
      -- Open over the origin pane, not the closing float (which may still be
      -- there). An entry recovered after a reload doesn't know its origin:
      -- wait for the float to go, then the tool has restored focus to it.
      if entry.origin_block then
        app.redirect[entry.block_id] = entry.origin_block
      elseif not gone(entry) then
        app.fail("close the open tool first")
      end
      local ok, res = pcall(app.open_tool, ctx, "goto", { zoomed = entry.zoomed })
      app.redirect[entry.block_id] = nil
      if not ok then error(res, 0) end
      return res
    end }

  app.action { name = "files", title = "Files…", long = "Browse files and open a terminal or editor",
    keywords = "files folders directory browse open edit", run = function(ctx)
      local _, origin = app.current(ctx)
      local cwd = app.cwd(ctx.session_id, origin.id)
      return app.open_tool(ctx, "files", { cwd = cwd, flags = { "--shell", app.settings.shell,
        "--editor-json", rex.json.encode(app.settings.editor), "--directory", cwd } })
    end }

  app.action { name = "keys", title = "Key guide", long = "Show what every key does",
    args = { mode = "string?" }, keywords = "help shortcuts keymap bindings", run = function(ctx, args)
      return app.open_tool(ctx, "keys", { flags = args.mode and { "--mode", args.mode } or nil })
    end }

  local function rename(target)
    return function(ctx)
      local view, origin = app.current(ctx)
      local id, current = origin.id, origin.label
      if target == "tab" then id, current = origin.window.window_id, origin.window.label end
      if target == "session" then id, current = view.session_id, view.label end
      return app.open_tool(ctx, "rename", { size = "dialog",
        flags = { "--target", target, "--id", id, "--current", current or "" } })
    end
  end
  for _, target in ipairs({ "pane", "tab", "session" }) do
    app.action { name = "rename_" .. target, title = "Rename " .. target .. "…", run = rename(target) }
  end

  -- Go to hands these over when it closes the place it was opened from, or
  -- makes a new session (internal/app/switcher/manage.go): both need this
  -- client, which only an action from a key can steer. ctx.block_id is Go
  -- to's own (closing) float.
  app.action { name = "close_place", title = "Close a pane, tab or session", internal = true,
    args = { kind = "string", session_id = "string", window_id = "string?", block_id = "string?" },
    run = function(ctx, args)
      local sid = args.session_id
      if args.kind == "session" then
        -- show another session first; the queued select runs once this returns
        for _, s in ipairs(app.call("session.list", {}).sessions or {}) do
          if s.session_id ~= sid then
            app.queue(ctx, "session.select", { session_id = s.session_id })
            break
          end
        end
        return app.call("session.destroy", { session_id = sid })
      elseif args.kind == "tab" then
        return app.call("session.close_window", { session_id = sid, window_id = args.window_id })
      end
      -- a pane: let the float go first, so focus does not land on it
      gone({ session_id = ctx.session_id, block_id = ctx.block_id })
      return app.call("block.close", { session_id = sid, block_id = args.block_id })
    end }

  app.action { name = "new_session", title = "New session", internal = true, args = { label = "string?" },
    run = function(ctx, args)
      local label = args.label ~= "" and args.label or nil
      local created = app.call("session.create", { label = label,
        initial_windows = { { layout = app.shell_layout(app.home) } } })
      local window = created.initial_windows and created.initial_windows[1]
      app.queue(ctx, "session.select", { session_id = created.session_id, window_id = window and window.window_id })
    end }

  local STARTS = { top = true, bottom = true, ["page-up"] = true, ["half-up"] = true }
  app.action { name = "scrollback", title = "Scrollback", long = "Browse the pane's scrollback in Neovim",
    args = { start = "string?" }, run = function(ctx, args)
      local start = STARTS[args.start or ""] and args.start or "bottom"
      return app.open_tool(ctx, "scrollback", { size = "whole", flags = { "--start", start } })
    end }

  -- Settings: the editor runs directly in a float; the file is created from
  -- the defaults (all commented out) the first time.
  local function settings_template()
    local function literal(value)
      if type(value) == "string" then return ("%q"):format(value) end
      if type(value) ~= "table" then return tostring(value) end
      local items = {}
      for i, item in ipairs(value) do items[i] = literal(item) end
      return "{ " .. table.concat(items, ", ") .. " }"
    end
    local lines = { "-- rexkit settings. Uncomment a line to change it, then reload the Rex",
      "-- config (Session mode ⇧R). Unknown names are ignored with a warning.", "return {" }
    for _, entry in ipairs(app.defaults) do
      lines[#lines + 1] = ("  -- %s = %s, -- %s"):format(entry[1], literal(entry[2]), entry[3])
    end
    lines[#lines + 1] = "}"
    return table.concat(lines, "\n") .. "\n"
  end

  app.action { name = "settings", title = "rexkit settings", long = "Edit rexkit settings", exits = true,
    keywords = "config preferences options", run = function(ctx)
      local path = app.paths.settings
      if not app.exists(path) then
        app.ensure_dir(app.paths.config)
        app.write_file(path, settings_template())
      end
      local _, origin = app.current(ctx)
      local command = {}
      for i, arg in ipairs(app.settings.editor) do command[i] = arg end
      command[#command + 1] = path
      return open_float(ctx, origin, { label = "rexkit:settings", command = command, cwd = app.paths.config })
    end }

  -- Next agent that needs you: the next blocked or failed agent after the
  -- focused pane, across all sessions (programs report it with OSC 7501).
  local NEEDS_YOU = { blocked = true, error = true }

  local function needs_you(sid, block)
    local status = app.terminal(sid, block, "program_status")
    for _, record in pairs(status and status.records or {}) do
      if NEEDS_YOU[record.state] then return true end
    end
    return false
  end

  app.action { name = "next_agent", title = "Next agent that needs you", exits = true,
    keywords = "agent blocked waiting attention question permission", run = function(ctx)
      local list = app.call("session.list", {})
      local order, here = {}, nil
      for _, session in ipairs(list.sessions or {}) do
        local attached = rex.call("session.attach", { session_id = session.session_id })
        local view = attached and attached.view
        for _, item in ipairs(view and app.blocks(view) or {}) do
          if item.id == ctx.block_id then here = #order + 1 end
          order[#order + 1] = { session_id = session.session_id, item = item }
        end
      end
      local start = here or 0
      for step = 1, #order do
        local candidate = order[(start + step - 1) % #order + 1]
        if candidate.item.id ~= ctx.block_id and needs_you(candidate.session_id, candidate.item.id) then
          app.call("session.focus_block", { session_id = candidate.session_id, block_id = candidate.item.id })
          app.queue(ctx, "session.select", { session_id = candidate.session_id,
            window_id = candidate.item.window.window_id })
          return { session_id = candidate.session_id, block_id = candidate.item.id }
        end
      end
      app.notify(ctx, "No agent needs you", "ok")
      return { found = false }
    end }

  -- Diagnostics for `rexkit doctor` (rex do rexkit_diagnostics --json).
  app.action { name = "diagnostics", title = "rexkit diagnostics", internal = true, run = function()
    local open = 0
    for _ in pairs(pickers) do open = open + 1 end
    return { root = app.root, binary = app.settings.binary, binary_present = app.exists(app.settings.binary),
      settings_file = app.paths.settings, state_dir = app.paths.state, settings = app.settings,
      warnings = app.warnings, modules = app.loaded, failed = app.failed, open_tools = open }
  end }
end
