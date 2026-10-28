-- Floating tools: open `rexkit <tool>` in a floating layer over the current
-- tab, then carry ⏎ / ⎋ / ⌃C to it through the "picker" mode so
-- that its result can switch the client to another session, which only a
-- key-run action can do. Normal panes never see this layer: the mode exists
-- only while a tool is open.
return function(app)
  local pickers = {} -- tool block id -> {tool, session_id, block_id, origin_block, zoomed, cols, dir, busy, queued}
  local handing = {} -- tool block id -> its entry, while the action its result asked for runs
  local tools_dir = app.paths.state .. "/tools"
  local KEY_BYTES = { enter = "\r", escape = "\27", ["ctrl+c"] = "\3", ["ctrl+f"] = "\6" }
  local DIALOG_COLS, DIALOG_ROWS = 56, 7 -- ui.DialogRows
  -- Tools that read ⏎ and ⎋ themselves (Neovim, an editor): no picker mode, no
  -- handshake. Their state dirs are still tracked, so they are GC'd.
  local OWN_KEYS = { scrollback = true, settings = true }
  -- Tools that read ⎋ and ⌃C themselves and close on their own, but answer
  -- the handshake for a global ⏎ (commit_via = "global") and for Go to,
  -- which closes them: no picker mode, so no mode panel while they are open.
  local NO_MODE = { hints = true }

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

  -- Rex (build 1541 on) draws a float exactly at its [x, y, w, h] bounds, as
  -- fractions of the window's layout area, and draws it as it draws a pane:
  -- a rounded surface with a header, whose cells take all but PAD_X of its
  -- width and PAD_Y of its height (pixels; internal/panes has the
  -- measurements). A tiled pane loses half a GAP at each edge inside the
  -- area; a float loses none. So the layout area, in pixels, follows from
  -- the tiled panes' sizes in cells and their cells' size in pixels.
  local PAD_X, PAD_Y, GAP, EPS = 40, 91, 16, 1e-6
  local WHOLE = { 0, 0, 1, 1 }
  -- last is the layout area last measured from panes the app owns. A pane
  -- it doesn't own (in a tab or session it isn't showing) reports 80×24
  -- cells of 8×16 pixels, whatever the window; every tab of the app's window
  -- has the same area.
  local last

  local function round(f) return math.floor(f * 10000 + 0.5) / 10000 end

  -- inside is the gap a tiled pane at `at`, `len` long (fractions), loses
  -- inside the area.
  local function inside(at, len)
    return (at > EPS and GAP / 2 or 0) + (at + len < 1 - EPS and GAP / 2 or 0)
  end

  -- area estimates the layout area from panes ({rect, size}). A pane's cells
  -- fill it to within a cell, so each pane puts the area's width and height
  -- in a range: the area is the middle of where the ranges meet (of the
  -- widest and the tallest pane's, should they not meet: a pane mid-resize).
  local function area(panes)
    local w, h, wide, tall = { 0, math.huge }, { 0, math.huge }, nil, nil
    for _, p in ipairs(panes) do
      local r, s = p.rect, p.size
      local x = (s.columns * s.cell_width_px + PAD_X + inside(r[1], r[3])) / r[3]
      local y = (s.rows * s.cell_height_px + PAD_Y + inside(r[2], r[4])) / r[4]
      p.w, p.h = { x, x + s.cell_width_px / r[3] }, { y, y + s.cell_height_px / r[4] }
      w = { math.max(w[1], p.w[1]), math.min(w[2], p.w[2]) }
      h = { math.max(h[1], p.h[1]), math.min(h[2], p.h[2]) }
      if not wide or r[3] > wide.rect[3] then wide = p end
      if not tall or r[4] > tall.rect[4] then tall = p end
    end
    if w[1] > w[2] then w = wide.w end
    if h[1] > h[2] then h = tall.h end
    local s = panes[1].size
    return { w = (w[1] + w[2]) / 2, h = (h[1] + h[2]) / 2, cw = s.cell_width_px, ch = s.cell_height_px }
  end

  -- measure estimates the layout area of a window of view (session sid)
  -- from its tiled panes, or from its zoomed pane zoomed_id alone, which
  -- fills it whatever its rect in the layout says; not from a float, which
  -- may be too new to have its size yet. Panes the app doesn't own say
  -- nothing of the window: then it is last, if any. owned says whether it
  -- was measured from panes the app owns.
  local function measure(sid, view, window, zoomed_id)
    local listed = {}
    if zoomed_id and zoomed_id ~= "" then
      listed[1] = { id = zoomed_id, rect = WHOLE }
    else
      for _, item in ipairs(app.tiled_blocks(view, window.window_id)) do
        listed[#listed + 1] = { id = item.id, rect = item.rect }
      end
    end
    local owned, unowned = {}, {}
    for _, p in ipairs(listed) do
      local size = app.terminal(sid, p.id, "size") or {}
      if (size.columns or 0) > 0 and (size.rows or 0) > 0 and (size.cell_width_px or 0) > 0
        and (size.cell_height_px or 0) > 0 and p.rect[3] > EPS and p.rect[4] > EPS then
        p.size = size
        table.insert(size.owner and owned or unowned, p)
      end
    end
    local a
    if #owned > 0 then
      a = area(owned)
      last = a
    else
      a = last or (#unowned > 0 and area(unowned)) or { w = 120 * 14 + PAD_X, h = 40 * 28 + PAD_Y, cw = 14, ch = 28 }
    end
    return { w = a.w, h = a.h, cw = a.cw, ch = a.ch, owned = #owned > 0 }
  end

  -- geometry measures the window item (a pane or a float of view) is in:
  -- its layout area w × h, its cells cw × ch, how many columns a float over
  -- all of it holds (cols), and item's own place in it (pane, in fractions:
  -- a tiled pane less its gaps, a float's bounds, or the whole window when
  -- item is zoomed).
  local function geometry(sid, view, item, zoomed)
    local window = item.window
    local g = measure(sid, view, window, zoomed and item.id or window.zoomed_block_id)
    local rect = zoomed and WHOLE or item.rect
    local x0, y0, x1, y1 = rect[1], rect[2], rect[1] + rect[3], rect[2] + rect[4]
    if item.layer.kind ~= "floating" then
      x0 = x0 + (x0 > EPS and GAP / 2 / g.w or 0)
      y0 = y0 + (y0 > EPS and GAP / 2 / g.h or 0)
      x1 = x1 - (x1 < 1 - EPS and GAP / 2 / g.w or 0)
      y1 = y1 - (y1 < 1 - EPS and GAP / 2 / g.h or 0)
    end
    g.cols = math.floor((g.w - PAD_X) / g.cw)
    g.pane = { x0, y0, x1 - x0, y1 - y0 }
    return g
  end

  -- holding is the size (fractions) of a float that holds cols × rows cells,
  -- at most the whole area. Its width has a cell to spare, against the
  -- estimate of the area (a column more does no harm), its height half of
  -- one, against rounding.
  local function holding(g, cols, rows)
    return math.min(1, ((cols + 1) * g.cw + PAD_X) / g.w), math.min(1, (rows * g.ch + PAD_Y + g.ch / 2) / g.h)
  end

  -- place returns the bounds of a float of size "float" (centred, sized by
  -- the window's width in columns, cols: most of a window 120 columns wide
  -- or more, all of a narrower one), "dialog" (DIALOG_COLS x DIALOG_ROWS
  -- cells centred over the pane, kept in the window), "pane" (exactly over
  -- the pane, so its cells line up with the pane's) or "whole".
  local function place(size, g, cols)
    local x, y, w, h
    if size == "pane" then
      x, y, w, h = g.pane[1], g.pane[2], g.pane[3], g.pane[4]
    elseif size == "dialog" then
      w, h = holding(g, DIALOG_COLS, DIALOG_ROWS)
      x = math.max(0, math.min(1 - w, g.pane[1] + (g.pane[3] - w) / 2))
      y = math.max(0, math.min(1 - h, g.pane[2] + (g.pane[4] - h) / 2))
    elseif size == "whole" or cols < 120 then
      -- in a narrower window a float would leave the panes beside it only
      -- a few columns, cut words that show nothing
      return WHOLE
    else
      w, h = 0.78, 0.74
      x, y = (1 - w) / 2, (1 - h) / 2
    end
    w, h = round(w), round(h)
    return { round(math.min(x, 1 - w)), round(math.min(y, 1 - h)), w, h }
  end

  local function terminal_layout(label, command, cwd, exit)
    return rex.layout.block { flavor = app.SHELL, label = label,
      options = { shell = "none", login = false, command = command, cwd = cwd or app.home,
        exit = exit or { on_completion = true } } }
  end

  -- Launching ------------------------------------------------------------------

  -- open_float opens spec.command in a new floating layer in the origin's
  -- window, focused unless spec.unfocused. spec: {label, command, cwd,
  -- bounds, unfocused}.
  local function open_float(ctx, origin, spec)
    local created = app.call("session.new_layer", { session_id = ctx.session_id, window_id = origin.window.window_id,
      bounds = spec.bounds, focus = not spec.unfocused, layout = terminal_layout(spec.label, spec.command, spec.cwd) })
    return { block_id = created.block_ids[1], layer_id = created.layer_id }
  end

  -- toast_cols is how wide the toast that argv runs is in a window cols wide,
  -- as `rexkit toast --measure` words it (ui.ToastWidth); failing that, the
  -- most it may take: 60% of the window, and argv loses its --title, which
  -- a rexkit older than this Lua doesn't know either. The toast cuts its
  -- message to the float it gets.
  local function toast_cols(argv, cols)
    local probe = {}
    for i, arg in ipairs(argv) do probe[i] = arg end
    probe[#probe + 1], probe[#probe + 2] = "--measure", tostring(cols)
    local measured = rex.exec(probe, { timeout = 2 })
    local n = measured and measured.ok and tonumber(measured.stdout:match("%d+"))
    if n and n > 0 then return n end
    for i = #argv - 1, 1, -1 do
      if argv[i] == "--title" then
        table.remove(argv, i)
        table.remove(argv, i)
      end
    end
    return math.max(1, math.floor(cols * 0.6))
  end

  -- shown measures the window the app shows, which sets last: that of the
  -- first session (first's, then the rest) whose active tab has panes the
  -- app owns. It reports whether there was one.
  local function shown(first)
    local ids = { first }
    for _, s in ipairs((rex.call("session.list", {}) or {}).sessions or {}) do ids[#ids + 1] = s.session_id end
    for _, id in ipairs(ids) do
      local view = rex.call("session.view", { session_id = id })
      local window = view and app.find_window(view, view.active_window_id)
      if window and measure(id, view, window, window.zoomed_block_id).owned then return true end
    end
    return false
  end

  -- notify shows a toast without taking focus: `rexkit toast` on a float of
  -- its own, a row tall, in the window's top-right corner, its title in the
  -- float's header, and returns {bounds}. Best effort: a missing or failing
  -- toast closes at once.
  -- opts (each optional): session_id, the session to show it in (default
  -- ctx's); title, the float's (default "rexkit"); status, an agent's
  -- state, which shapes the toast; go, the {session_id, window_id,
  -- block_id} a click on it goes to; client, the app's client, which that
  -- click switches to another session.
  function app.notify(ctx, message, kind, opts)
    opts = opts or {}
    local sid = opts.session_id or ctx.session_id
    if not (sid and app.exists(app.settings.binary)) then return end
    local view = rex.call("session.view", { session_id = sid })
    local window = view and app.find_window(view, view.active_window_id)
    if not window then return end
    local g = measure(sid, view, window, window.zoomed_block_id)
    if not g.owned and not last and shown(ctx.session_id) then g = measure(sid, view, window, window.zoomed_block_id) end
    local cols = math.floor((g.w - PAD_X) / g.cw)
    local argv = { app.settings.binary, "toast", "--message", message, "--kind", kind or "info",
      "--title", opts.title or "rexkit" }
    local function flag(name, value)
      if value and value ~= "" then
        argv[#argv + 1] = "--" .. name
        argv[#argv + 1] = value
      end
    end
    flag("status", opts.status)
    if opts.go and opts.go.session_id and opts.go.block_id then
      flag("session", sid)
      flag("origin-window", window.window_id) -- the tab it shows in: a click elsewhere switches tabs
      flag("go-session", opts.go.session_id)
      flag("go-window", opts.go.window_id)
      flag("go-block", opts.go.block_id)
      flag("client", opts.client)
    end
    local w, h = holding(g, toast_cols(argv, cols), 1)
    w, h = round(w), round(h)
    local bounds = { round(1 - w), 0, w, h }
    rex.call("session.new_layer", { session_id = sid, window_id = window.window_id, bounds = bounds, focus = false,
      layout = terminal_layout("rexkit:toast", argv, nil, { on_completion = true, quick_exit_threshold_ms = 0 }) })
    return { bounds = bounds }
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
  -- replaces another one, whose float ended the zoom). opts.cols is the
  -- window's width in columns as that tool measured it: a pane zoomed again
  -- when the float over it closed gets its zoomed size from the app only a
  -- moment later, so it would measure the window by its unzoomed width.
  -- opts.reads says the tool reads the origin's screen: over a zoomed
  -- origin its float opens without focus, which would end the zoom, and the
  -- app would give the pane its tiled size and rewrap its screen before the
  -- tool has read it; the tool takes focus once it has (--take-focus).
  function app.open_tool(ctx, tool, opts)
    opts = opts or {}
    local view, origin = app.current(ctx)
    if (origin.label or ""):match("^rexkit:") and origin.layer.kind == "floating" then
      app.fail("close the open tool first")
    end
    app.exit_modes(ctx) -- a mode would block the tool's typing
    prune()
    local bin = binary()
    local dir = new_state_dir()
    local window = origin.window
    local zoomed = opts.zoomed or window.zoomed_block_id == origin.id
    local unfocused = opts.reads and window.zoomed_block_id == origin.id
    local argv = { bin, tool, "--session", ctx.session_id, "--origin-block", origin.id,
      "--origin-window", window.window_id, "--state", dir }
    if zoomed then argv[#argv + 1] = "--zoomed" end
    if unfocused then argv[#argv + 1] = "--take-focus" end
    if ctx.client_id then
      argv[#argv + 1] = "--client"
      argv[#argv + 1] = ctx.client_id
    end
    if not app.settings.dismiss_on_blur then argv[#argv + 1] = "--dismiss-on-blur=false" end
    for _, flag in ipairs(opts.flags or {}) do argv[#argv + 1] = flag end
    local g = geometry(ctx.session_id, view, origin, zoomed)
    local cols = opts.cols or g.cols
    local ok, opened = pcall(open_float, ctx, origin, { label = "rexkit:" .. tool, command = argv, cwd = opts.cwd,
      bounds = place(opts.size or "float", g, cols), unfocused = unfocused })
    if not ok then
      remove_state_dir(dir)
      error(opened, 0)
    end
    pickers[opened.block_id] = { tool = tool, session_id = ctx.session_id, block_id = opened.block_id,
      origin_block = origin.id, zoomed = zoomed, cols = cols, dir = dir }
    if not (OWN_KEYS[tool] or NO_MODE[tool]) and app.settings.commit_via == "mode" then
      app.queue(ctx, "client.mode.enter", { name = "picker" })
    end
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
    handing[entry.block_id] = entry
    local ok, err = pcall(function()
      local run = app.actions[result.invoke]
      if run then return run(ctx, result.args or {}) end
      local _, failure = rex.invoke(result.invoke, result.args or {})
      if failure then app.fail(app.clean(failure)) end
    end)
    app.redirect[entry.block_id] = nil
    handing[entry.block_id] = nil
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
        if app.reveal_float then -- a floating pane ⌃P w hid comes back with its tab's
          app.reveal_float(result.session_id, result.window_id, result.block_id)
        end
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
  -- A ⏎ (and Go to's ⌃F) goes with the one-shot commit marker <state>/commit
  -- (popup.CommitFile), created (atomically) right before the byte is
  -- written and removed after the await, whatever happened: the tool takes
  -- it with the key it reads, and so tells this one, whose result is read
  -- here, from one typed while the picker mode was not active (after a
  -- reload), whose result nobody reads.
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
      local marked = next_bytes == KEY_BYTES.enter or next_bytes == KEY_BYTES["ctrl+f"]
      local marker = marked and entry.dir .. "/commit" or nil
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

  -- ⌃F: in Go to, Search with Go to's query (internal/app/switcher hands it
  -- over as a result that invokes rexkit_search). In any other tool it is
  -- that tool's own key (Files' recursive search), written into it as typed.
  local function search_text(ctx)
    local entry = lookup(ctx, true)
    if not entry then return miss(ctx, "ctrl+f") end
    if entry.tool == "goto" then return handshake(ctx, entry, KEY_BYTES["ctrl+f"]) end
    local _, err = app.terminal(entry.session_id, entry.block_id, "write", { data = KEY_BYTES["ctrl+f"] })
    if err then app.fail(entry.tool .. " is gone") end
    return { forwarded = entry.tool }
  end

  app.action { name = "commit", title = "Confirm in tool", internal = true,
    args = { key = "string?", global = "boolean?" }, run = commit }
  app.action { name = "dismiss", title = "Back / close tool", internal = true,
    args = { key = "string?" }, run = dismiss }
  app.action { name = "search_text", title = "Search every pane from Go to", internal = true,
    run = search_text }

  if app.settings.commit_via == "global" then
    app.bind(nil, "enter", "rexkit_commit", { key = "enter", global = true })
  else -- the picker mode: only these keys go through Lua, and only while a tool is open
    rex.mode("picker", { exclusive = false })
    app.bind("picker", "enter", "rexkit_commit", { key = "enter" })
    app.bind("picker", "ctrl+shift+space", "rexkit_goto") -- see rexkit_goto: works in a stale mode too
    app.bind("picker", "escape", "rexkit_dismiss", { key = "escape" })
    app.bind("picker", "ctrl+c", "rexkit_dismiss", { key = "ctrl+c" })
    app.bind("picker", "ctrl+f", "rexkit_search_text")
  end

  -- hints_key: a global key for Hints (core.lua's key settings). Hints
  -- itself needs no keys of Lua's: with no mode on, ⇥, ⎋, ⌃C and its
  -- letters reach it as they reach any terminal.
  app.key_settings.hints_key = { action = "rexkit_hints", does = "opens Hints" }

  -- The toast a tool shows when it is done (`rexkit hints`: Copied …, Pasted …).
  app.action { name = "notify", title = "Show a toast", internal = true,
    args = { message = "string", kind = "string?", title = "string?" }, run = function(ctx, args)
      return app.notify(ctx, args.message, args.kind, { title = args.title })
    end }

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
  -- goto_flags name the next-agent key (attend.lua) for Needs you and Go
  -- to's key panel, which say it moves the selection there.
  local function goto_flags()
    return { "--next-key", app.agent_keys and app.agent_keys.next or "" }
  end

  app.action { name = "goto", title = "Go to…", long = "Go to a session, tab, pane or agent",
    keywords = "switch jump find session tab pane agent", run = function(ctx)
      local entry = lookup(ctx, true)
      if not entry then return app.open_tool(ctx, "goto", { flags = goto_flags() }) end -- open_tool leaves any mode first
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
      local ok, res = pcall(app.open_tool, ctx, "goto", { zoomed = entry.zoomed, cols = entry.cols, flags = goto_flags() })
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

  app.action { name = "hints", title = "Hints…", long = "Copy, open or paste a URL, path or SHA from the pane",
    keywords = "hints quick select copy open paste url path sha link", run = function(ctx)
      local _, origin = app.current(ctx)
      local cwd = app.cwd(ctx.session_id, origin.id)
      return app.open_tool(ctx, "hints", { size = "pane", reads = true, cwd = cwd, flags = {
        "--directory", cwd, "--editor-json", rex.json.encode(app.settings.editor) } })
    end }

  -- F1 is global, so it never reaches a tool. Search lists its own keys on
  -- F1 (its ? is part of a regular expression), so there it is carried to
  -- the tool as the key's bytes instead of opening the guide over it.
  local OWN_F1 = { search = true }

  app.action { name = "keys", title = "Key guide", long = "Show what every key does",
    args = { mode = "string?" }, keywords = "help shortcuts keymap bindings", run = function(ctx, args)
      local entry = not args.mode and lookup(ctx, true)
      if entry and OWN_F1[entry.tool] then
        local _, err = app.terminal(entry.session_id, entry.block_id, "write", { data = "\27OP" })
        if not err then return { forwarded = entry.tool } end
      end
      local flags = args.mode and { "--mode", args.mode } or {}
      -- The key settings' keys lead their rows (⌃⇧A before ⌥A), as toasts
      -- and Go to name them.
      local first = {}
      for _, entry in ipairs(app.defaults) do
        first[#first + 1] = app.setting_keys[entry[1]]
      end
      if #first > 0 then
        flags[#flags + 1], flags[#flags + 2] = "--first-json", rex.json.encode(first)
      end
      return app.open_tool(ctx, "keys", { flags = flags })
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
      return app.open_tool(ctx, "scrollback", { size = "pane", flags = { "--start", start } })
    end }

  -- Search every pane. ⏎ on a match hands rexkit_search_jump the pane and
  -- the capture it searched (internal/app/search/act.go), whose line
  -- numbers are the ones Scrollback opens at.
  --
  -- Go to's ⌃F (internal/app/switcher/search.go) runs this with query, its
  -- query, and after, its own block: Search opens once that float is gone,
  -- so that closing it takes neither the focus nor the zoom from Search,
  -- over the pane Go to restored, as if opened there, and as big as Go to;
  -- it fails if the float is still there. Without the handshake Go to
  -- starts this itself through `rex do -C <its client> --origin key`, which
  -- queues the picker mode for that client (the app takes it only with
  -- Remote Control on) but runs with a client of its own: client names Go
  -- to's, for Search's jumps to other sessions. Nobody reads that run's
  -- result, so a failure there shows as a toast too.
  app.action { name = "search", title = "Search every pane…",
    long = "Search the screen and scrollback of every pane",
    args = { query = "string?", after = "string?", client = "string?" },
    keywords = "search find grep scrollback history every all panes sessions output", run = function(ctx, args)
      local ok, res = pcall(function()
        local from = args.after and (handing[args.after] or pickers[args.after])
        if (args.after or "") ~= "" and not gone({ session_id = ctx.session_id, block_id = args.after }) then
          app.fail("Go to is still closing")
        end
        local own = ctx
        if (args.client or "") ~= "" then
          own = {}
          for k, v in pairs(ctx) do own[k] = v end
          own.client_id = args.client
        end
        local flags = {}
        if args.query and args.query ~= "" then flags = { "--query", args.query } end
        return app.open_tool(own, "search", { cols = from and from.cols, flags = flags })
      end)
      if ok then return res end
      if (args.client or "") ~= "" then
        app.notify(ctx, "Can't open Search: " .. (app.clean(res):gsub("^rexkit: ", "")), "error")
      end
      error(res, 0)
    end }

  -- Opens Scrollback over the pane Search went to, at the line, with the
  -- query as its search (n and N go on from there). By the time this runs
  -- the pane has focus and its session is about to be shown; ctx.block_id
  -- is Search's own (closing) float, which must be gone first, or closing
  -- it would take focus from Scrollback.
  app.action { name = "search_jump", title = "Open a search result in Scrollback", internal = true,
    args = { session_id = "string", window_id = "string?", block_id = "string", page = "string",
      line = "integer?", search = "string?", regex = "boolean?", match_case = "boolean?" },
    run = function(ctx, args)
      -- the page is a page.txt in a folder of its own under tools_dir
      local page = tostring(args.page or ""):gsub("/+", "/")
      local root = tools_dir:gsub("/+", "/")
      if page:sub(1, #root + 1) ~= root .. "/" or not page:sub(#root + 2):match("^[^/]+/page%.txt$")
        or page:find("/%.%.?/") then
        app.fail("search_jump: the page is not in " .. tools_dir)
      end
      local function drop() -- it is ours until Scrollback takes it
        os.remove(page)
        os.remove((page:gsub("/[^/]+$", "")))
      end
      gone({ session_id = ctx.session_id, block_id = ctx.block_id })
      local view = rex.call("session.view", { session_id = args.session_id })
      if not (view and app.find_block(view, args.block_id)) then
        drop()
        app.fail("that pane closed")
      end
      local flags = { "--page", page, "--line", tostring(math.max(0, math.floor(tonumber(args.line) or 0))) }
      if args.search and args.search ~= "" then
        flags[#flags + 1] = "--search"
        flags[#flags + 1] = args.search
        if args.regex then flags[#flags + 1] = "--regex" end
        if args.match_case then flags[#flags + 1] = "--match-case" end
      end
      local there = { session_id = args.session_id, block_id = args.block_id, client_id = ctx.client_id,
        origin = ctx.origin }
      local ok, res = pcall(app.open_tool, there, "scrollback", { size = "pane", flags = flags })
      if not ok then
        drop()
        error(res, 0)
      end
      return res
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
      local view, origin = app.current(ctx)
      local command = {}
      for i, arg in ipairs(app.settings.editor) do command[i] = arg end
      command[#command + 1] = path
      local g = geometry(ctx.session_id, view, origin, origin.window.zoomed_block_id == origin.id)
      return open_float(ctx, origin, { label = "rexkit:settings", command = command, cwd = app.paths.config,
        bounds = place("float", g, g.cols) })
    end }

  -- Diagnostics for `rexkit doctor` (rex do rexkit_diagnostics --json).
  app.action { name = "diagnostics", title = "rexkit diagnostics", internal = true, run = function()
    local open = 0
    for _ in pairs(pickers) do open = open + 1 end
    return { root = app.root, binary = app.settings.binary, binary_present = app.exists(app.settings.binary),
      settings_file = app.paths.settings, state_dir = app.paths.state, settings = app.settings,
      warnings = app.warnings, modules = app.loaded, failed = app.failed, open_tools = open,
      attend = app.attend_token and { token = app.attend_token, keys = app.agent_keys } }
  end }
end
