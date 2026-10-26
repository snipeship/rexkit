-- The keyboard modes (core.lua's app.MODES: the Zellij-style modes, the tmux
-- prefix and locked, as far as zellij_modes and tmux_prefix turn them on).
-- Each prefix enters its mode from normal mode natively.
-- The persistent modes are not exclusive, so global shortcuts (⌥H, ⌥N, F1,
-- ⌃⇧Space …) and every ⌘ key keep working in them; each one blocks the text
-- keys it doesn't bind (app.BLOCK_KEYS), so typing can't leak into the
-- terminal. Each binds its own keys plus:
--   ⎋ ⏎ ⌃C and its own prefix   leave the mode
--   the other prefixes          switch straight to that mode
--   ?                           the key guide for the mode
-- Tmux is one key at a time (once) and exclusive: every key in
-- app.TMUX_KEYS, and every global key, that it doesn't bind leaves it, as an
-- unbound key does in tmux.
-- Locked passes every global rexkit key through to the terminal; only ⌃G
-- leaves it. Loaded last, because locked and the block lists need the final
-- list of bindings.
return function(app)
  -- block_unbound: in each persistent mode, block every text key the mode
  -- doesn't bind and the root doesn't either.
  local function block_unbound(bound, root)
    for _, mode in ipairs(app.MODES) do
      if not (mode.once or mode.passthrough) then
        local own = bound[mode.name] or {}
        for _, key in ipairs(app.BLOCK_KEYS) do
          if not own[key] and not root[key] then rex.unbind(mode.name .. "/" .. key) end
        end
      end
    end
  end

  local switch = {} -- mode name -> action that leaves the current mode and enters it

  for _, mode in ipairs(app.MODES) do
    rex.mode(mode.name, { exclusive = mode.once == true })
    local enter = { name = mode.name, once = mode.once }
    app.bind(nil, mode.key, "client.mode.enter", enter)
    switch[mode.name] = app.action { name = "mode_" .. mode.name, title = mode.title, run = function(ctx)
      app.queue(ctx, "client.mode.exit_all")
      app.queue(ctx, "client.mode.enter", enter)
    end }
  end

  local function bind_exits(name)
    for _, key in ipairs({ "escape", "enter", "ctrl+c" }) do app.bind(name, key, "client.mode.exit_all") end
  end

  for _, mode in ipairs(app.MODES) do
    if not mode.passthrough then
      bind_exits(mode.name)
      for _, other in ipairs(app.MODES) do
        if other.name ~= mode.name then
          app.bind(mode.name, other.key, switch[other.name])
        elseif not mode.once then -- tmux's own ⌃B sends ⌃B (bindings.lua)
          app.bind(mode.name, other.key, "client.mode.exit_all")
        end
      end
      if mode.name ~= "session" then -- session's ? opens the whole guide
        app.bind(mode.name, "shift+/", "rexkit_keys", { mode = mode.name })
      end
    end
  end

  -- What each mode binds by now, and the root keys (which stay live in the
  -- non-exclusive modes).
  local bound, root = {}, {}
  for _, binding in ipairs(rex.bindings()) do
    local set = binding.mode and (bound[binding.mode] or {}) or root
    set[binding.key] = true
    if binding.mode then bound[binding.mode] = set end
  end

  -- Locked: every global key rexkit bound goes to the terminal instead.
  for key in pairs(root) do
    if key ~= app.prefix.locked then app.bind("locked", key, "pane.send_key", { key = key }) end
  end
  app.bind("locked", "escape", "pane.send_key", { key = "escape" })
  app.bind("locked", app.prefix.locked, "client.mode.exit_all")

  -- Tmux: an unbound key is still one of its keys, so `once` leaves the mode.
  -- Plumbing (not listed in the key guide).
  if app.prefix.tmux then
    local leave = app.action { name = "tmux_unbound", title = "Leave the Tmux prefix", internal = true,
      run = function(ctx) app.exit_modes(ctx) end }
    local tmux_keys = {}
    for _, key in ipairs(app.TMUX_KEYS) do tmux_keys[#tmux_keys + 1] = key end
    for key in pairs(root) do tmux_keys[#tmux_keys + 1] = key end -- ⌃B ⌥H is not ⌥H, but must not stick either
    local caught = {}
    for _, key in ipairs(tmux_keys) do
      if not (bound.tmux or {})[key] and not caught[key] then
        caught[key] = true
        app.bind("tmux", key, leave)
      end
    end
  end

  block_unbound(bound, root)
end
