-- Agent attention: ⌥A goes to the next agent that needs you (blocked, failed,
-- or done and not looked at yet), ⌥⇧A goes back to where those presses
-- began, and a watcher (`rexkit attend`, one per config load) toasts when an
-- agent elsewhere needs you. The Go side keeps what was seen and where the
-- walk began ($XDG_STATE_HOME/rexkit/attention.json); this side runs the
-- jumps, which only an action from a key can do across sessions.
return function(app)
  -- token names this config load. The watcher it starts asks for it, so a
  -- watcher started by an older load, or by `rex config check`, ends.
  local function new_token()
    local file = io.open("/dev/urandom", "rb")
    local bytes = file and file:read(8)
    if file then file:close() end
    if not bytes or #bytes < 8 then
      return ("%d-%d"):format(os.time(), math.random(1, 2 ^ 30))
    end
    return (bytes:gsub(".", function(c) return ("%02x"):format(c:byte()) end))
  end
  local token = new_token()
  app.attend_token = token

  -- The keys toasts name: ⌥A and ⌥⇧A, or Session mode's a and ⇧A when the
  -- alt shortcuts are off.
  local session = app.prefix.session
  local next_key = app.settings.alt_shortcuts and "alt+a" or (session and session .. " a") or ""
  local back_key = app.settings.alt_shortcuts and "alt+shift+a" or (session and session .. " shift+a") or ""

  -- Toasts -----------------------------------------------------------------------

  local STATES = { blocked = "blocked", failed = "failed", done = "done" }

  local function toast_states()
    if not app.settings.agent_toasts then return "" end
    local list = {}
    for _, name in ipairs(app.settings.agent_toast_states) do
      if STATES[name] then list[#list + 1] = name end
    end
    return table.concat(list, ",")
  end

  -- start runs the watcher, detached: it outlives this load until the
  -- server runs another one (it then ends on its own), stops, or forgets
  -- rexkit. The config checks of `rexkit install` and `rexkit doctor`
  -- (strict, or with REXKIT_CHECK set) start none; a plain `rex config
  -- check` starts one that ends once the server does not confirm it.
  local function start()
    if rawget(_G, "rexkit_strict") or os.getenv("REXKIT_CHECK") or not app.exists(app.settings.binary) then return end
    local ok, err = pcall(rex.exec.start, { app.settings.binary, "attend", "--token", token,
      "--next-key", next_key, "--toasts", toast_states() }, { detach = true, stdout = false, stderr = false })
    if not ok then app.warn("the agent watcher did not start: " .. app.clean(err)) end
  end

  -- The watcher's plumbing (internal/attend/watcher.go).
  app.action { name = "attend_token", title = "Agent watcher's token", internal = true,
    run = function() return { token = token } end }

  -- A toast in the active window of the session it runs in. The watcher
  -- runs it in each session: run by `rex do`, an action reaches no other.
  app.action { name = "attend_toast", title = "Agent toast", internal = true,
    args = { message = "string", kind = "string?", status = "string?",
      go_session = "string?", go_window = "string?", go_block = "string?", client = "string?" },
    run = function(ctx, args)
      local go = args.go_block and { session_id = args.go_session, window_id = args.go_window, block_id = args.go_block }
      app.notify(ctx, args.message, args.kind, { status = args.status, client = args.client, go = go })
    end }

  -- Jumps ------------------------------------------------------------------------

  -- walk asks `rexkit attend <which>` where to go from the pane in front, goes
  -- there and shows the toast it names where the user lands.
  local function walk(ctx, which)
    local _, here = app.current(ctx)
    local label = here.label or ""
    if which == "next" and here.layer.kind == "floating" and label == "rexkit:goto" then
      -- Go to is open: ⌥A picks its next agent that needs you instead. Leave
      -- Session mode (⌃O a), which would swallow Go to's typing; ⏎ still
      -- needs the picker mode.
      app.terminal(ctx.session_id, here.id, "write", { data = "\27a" })
      app.exit_modes(ctx)
      if app.settings.commit_via == "mode" then app.queue(ctx, "client.mode.enter", { name = "picker" }) end
      return { go_to = true }
    elseif here.layer.kind == "floating" and label:match("^rexkit:") then
      app.fail("close the open tool first")
    end
    app.exit_modes(ctx)
    if not app.exists(app.settings.binary) then app.fail(app.settings.binary .. " is missing; reinstall rexkit") end
    local done, err = rex.exec({ app.settings.binary, "attend", which, "--session", ctx.session_id,
      "--window", here.window.window_id, "--block", here.id, "--next-key", next_key, "--back-key", back_key },
      { timeout = 10 })
    if not (done and done.ok) then
      app.fail("attend " .. which .. ": " .. tostring(done and done.stderr or err))
    end
    local jump = rex.json.decode(done.stdout)
    local sid = jump.session_id
    if sid then
      if jump.block_id then
        app.call("session.focus_block", { session_id = sid, block_id = jump.block_id })
      elseif jump.window_id then
        app.call("session.focus_window", { session_id = sid, window_id = jump.window_id })
      end
      app.queue(ctx, "session.select", { session_id = sid, window_id = jump.window_id })
    end
    if jump.toast then
      app.notify(ctx, jump.toast.message, jump.toast.kind, { session_id = sid, status = jump.toast.status })
    end
    return jump
  end

  app.action { name = "next_agent", title = "Next agent that needs you",
    long = "Go to the next agent that is blocked, failed, or done and not seen",
    keywords = "agent blocked waiting attention question permission done failed", run = function(ctx)
      return walk(ctx, "next")
    end }

  app.action { name = "agent_back", title = "Back to where you were",
    long = "Go back to where you were before the next-agent key",
    keywords = "agent back return attention", run = function(ctx)
      return walk(ctx, "back")
    end }

  start()
end
