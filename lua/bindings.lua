-- Every key rexkit binds, by mode. Keys that keep the mode active (focus,
-- move, resize, tab switching, prompt and search steps) are bound to native
-- client actions where Rex has one; anything that opens something or changes
-- the layout leaves the mode first, since a mode blocks the text keys that
-- would follow. Mode entry/exit keys live in modes.lua.
--
-- The app's mode panel titles a row with its action's title (a binding has no
-- title of its own), so a key whose effect depends on its args gets a thin
-- action of its own ("Grow left", "Tab 3"); the parameterised actions stay
-- for the palette and for scripts.
return function(app)
  local bind, cmd, action = app.bind, app.command, app.action
  local SIDES = { "left", "down", "up", "right" }
  local DIRECTIONS = { h = "left", j = "down", k = "up", l = "right",
    left = "left", down = "down", up = "up", right = "right" }

  -- Lua-backed actions -----------------------------------------------------------

  local grow = action { name = "grow", title = "Grow pane", repeats = true, args = { direction = "string?" },
    run = function(ctx, args) return app.resize(ctx, args.direction, false) end }
  local shrink = action { name = "shrink", title = "Shrink pane", repeats = true, args = { direction = "string?" },
    run = function(ctx, args) return app.resize(ctx, args.direction, true) end }
  local swap_next = action { name = "swap_next", title = "Swap with next pane", repeats = true,
    run = function(ctx) return app.swap(ctx, 1) end }
  local swap_previous = action { name = "swap_previous", title = "Swap with previous pane", repeats = true,
    run = function(ctx) return app.swap(ctx, -1) end }
  local tab_next = action { name = "tab_next", title = "Next tab", repeats = true,
    run = function(ctx) return app.tab_step(ctx, 1) end }
  local tab_previous = action { name = "tab_previous", title = "Previous tab", repeats = true,
    run = function(ctx) return app.tab_step(ctx, -1) end }
  action { name = "tab_goto", title = "Go to tab", exits = true, args = { index = "integer" },
    run = function(ctx, args) return app.tab_goto(ctx, args.index) end }
  local last_tab = action { name = "last_tab", title = "Last tab", long = "Switch to the tab you were on before",
    run = app.last_tab }
  local pane_to_previous = action { name = "pane_to_previous_tab", title = "To previous tab",
    long = "Move pane to the previous tab", exits = true, run = function(ctx) return app.break_pane(ctx, -1) end }
  local pane_to_next = action { name = "pane_to_next_tab", title = "To next tab",
    long = "Move pane to the next tab", exits = true, run = function(ctx) return app.break_pane(ctx, 1) end }

  -- One action per direction / index / start, for distinct panel rows.
  local focus_to, grow_to, shrink_to, scrollback_from, tab_number = {}, {}, {}, {}, {}
  for _, d in ipairs(SIDES) do
    focus_to[d] = action { name = "focus_" .. d, title = "Focus " .. d, repeats = true,
      keywords = "focus pane " .. d, run = function(ctx)
        app.queue(ctx, "pane.focus", { direction = d, wrap = false })
        return { focused = d }
      end }
    grow_to[d] = action { name = "grow_" .. d, title = "Grow " .. d, repeats = true,
      keywords = "resize grow pane " .. d, run = function(ctx) return app.resize(ctx, d, false) end }
    shrink_to[d] = action { name = "shrink_" .. d, title = "Shrink " .. d, repeats = true,
      keywords = "resize shrink pane " .. d, run = function(ctx) return app.resize(ctx, d, true) end }
  end
  for i = 1, 9 do
    tab_number[i] = action { name = "tab_goto_" .. i, title = "Tab " .. i, exits = true,
      keywords = "go to tab " .. i, run = function(ctx) return app.tab_goto(ctx, i) end }
  end
  for _, start in ipairs({ { "bottom", "Scrollback from bottom" }, { "top", "Scrollback from top" },
    { "half-up", "Scrollback, half page up" }, { "page-up", "Scrollback, page up" } }) do
    local s = start[1]
    scrollback_from[s] = action { name = "scrollback_" .. s:gsub("-", "_"), title = start[2],
      keywords = "scrollback history neovim", run = function(ctx)
        return app.actions.rexkit_scrollback(ctx, { start = s })
      end }
  end

  local scratch = action { name = "scratch", title = "Show / hide Scratch", long = "Visit the Scratch tab or go back",
    exits = true, run = app.toggle_scratch }
  local park = action { name = "scratch_park", title = "Park / restore", exits = true,
    long = "Move the pane to Scratch, or bring it back to where it was", run = app.park_or_restore }

  -- Pane mode (⌃P) ---------------------------------------------------------------

  for key, direction in pairs(DIRECTIONS) do bind("pane", key, focus_to[direction]) end
  bind("pane", "p", "pane.focus_next")
  bind("pane", "shift+p", "pane.focus_previous")
  for _, split in ipairs({
    { "r", "right", "Split right" }, { "d", "down", "Split down" },
    { "shift+r", "left", "Split left" }, { "shift+d", "up", "Split up" }, { "n", "auto", "New pane" },
  }) do
    bind("pane", split[1], cmd("split_" .. split[2], split[3], "pane.split", { direction = split[2] }))
  end
  bind("pane", "f", cmd("zoom", "Zoom / restore", "pane.zoom"))
  bind("pane", "x", cmd("close_pane", "Close pane", "pane.close"))
  bind("pane", "=", cmd("balance", "Balance splits", "pane.balance"))
  bind("pane", "i", cmd("insert_path", "Insert path", "pane.insert_path"))
  bind("pane", "g", cmd("go_to_directory", "Go to folder", "pane.go_to_directory"))
  bind("pane", "c", "rexkit_rename_pane")
  bind("pane", "w", scratch)
  bind("pane", "e", park)
  bind("pane", "o", "rexkit_files")

  -- Tab mode (⌃T) ------------------------------------------------------------------

  for _, key in ipairs({ "h", "k", "left", "up" }) do bind("tab", key, tab_previous) end
  for _, key in ipairs({ "l", "j", "right", "down" }) do bind("tab", key, tab_next) end
  bind("tab", "tab", last_tab)
  bind("tab", "shift+h", "client.tab.move.backward")
  bind("tab", "shift+l", "client.tab.move.forward")
  for i = 1, 9 do bind("tab", tostring(i), tab_number[i]) end
  bind("tab", "n", cmd("tab_new", "New tab", "client.tab.new"))
  bind("tab", "x", cmd("tab_close", "Close tab", "client.tab.close"))
  bind("tab", "r", "rexkit_rename_tab")
  bind("tab", "b", cmd("pane_to_new_tab", "To new tab", "pane.move_to_new_tab"))
  bind("tab", "[", pane_to_previous)
  bind("tab", "]", pane_to_next)
  bind("tab", "m", cmd("pane_to_tab", "Choose tab for pane", "pane.move_to_tab"))

  -- Resize mode (⌃N): h/j/k/l grow towards that side, ⇧ shrinks from it,
  -- = and + grow, - shrinks, 0 balances.

  for key, direction in pairs(DIRECTIONS) do
    bind("resize", key, grow_to[direction])
    bind("resize", "shift+" .. key, shrink_to[direction])
  end
  bind("resize", "=", grow)
  bind("resize", "shift+=", grow)
  bind("resize", "-", shrink)
  bind("resize", "0", "pane.balance")

  -- Move mode (⌃H) -------------------------------------------------------------------

  for key, direction in pairs(DIRECTIONS) do bind("move", key, "pane.move." .. direction) end
  bind("move", "n", swap_next)
  bind("move", "tab", swap_next)
  bind("move", "p", swap_previous)
  bind("move", "shift+tab", swap_previous)

  -- Scroll mode (⌃S): E/U/D/B/PgUp/PgDn open the scrollback viewer there.

  for key, start in pairs({ e = "bottom", d = "bottom", pagedown = "bottom", b = "top", u = "half-up",
    pageup = "page-up" }) do
    bind("scroll", key, scrollback_from[start])
  end
  bind("scroll", "k", "client.prompt.previous")
  bind("scroll", "up", "client.prompt.previous")
  bind("scroll", "j", "client.prompt.next")
  bind("scroll", "down", "client.prompt.next")
  local find = cmd("find", "Find", "client.find.open")
  bind("scroll", "/", find)
  bind("scroll", "s", find)
  bind("scroll", "n", "client.find.next")
  bind("scroll", "shift+n", "client.find.previous")
  bind("scroll", "p", "client.find.previous")
  bind("scroll", "q", "client.mode.exit_all")

  -- Session mode (⌃O) -----------------------------------------------------------------

  bind("session", "w", "rexkit_goto")
  bind("session", "a", "rexkit_next_agent")
  bind("session", "n", cmd("session_new", "New session", "session.new"))
  bind("session", "r", "rexkit_rename_session")
  bind("session", "j", cmd("session_next", "Next session", "session.next"))
  bind("session", "k", cmd("session_previous", "Previous session", "session.previous"))
  bind("session", "c", "rexkit_settings")
  bind("session", "shift+/", "rexkit_keys")
  bind("session", "s", cmd("rex_settings", "Rex settings", "client.settings"))
  bind("session", "p", cmd("palette", "Command palette", "client.palette.toggle"))
  bind("session", "b", cmd("sidebar", "Toggle sidebar", "client.sidebar.toggle"))
  bind("session", "d", cmd("detach", "Close this window", "client.window.close"))
  bind("session", "shift+r", cmd("reload", "Reload config", "client.config.reload"))

  -- Tmux prefix (⌃B, one key then back to normal) ---------------------------------------

  for key, direction in pairs(DIRECTIONS) do bind("tmux", key, focus_to[direction]) end
  bind("tmux", "c", "client.tab.new")
  bind("tmux", "n", tab_next) -- Lua, so ⌃T Tab knows the tab you left
  bind("tmux", "p", tab_previous)
  bind("tmux", "z", "pane.zoom")
  bind("tmux", "x", "pane.close")
  bind("tmux", "o", "pane.focus_next")
  bind("tmux", ",", "rexkit_rename_tab")
  bind("tmux", "shift+5", "pane.split.right")
  bind("tmux", "shift+'", "pane.split.down")
  bind("tmux", "d", "client.window.close")
  if app.prefix.scroll then bind("tmux", "[", "rexkit_mode_scroll") end
  bind("tmux", "ctrl+b", "pane.send_key", { key = "ctrl+b" })

  -- Global -----------------------------------------------------------------------------

  bind(nil, "ctrl+shift+space", "rexkit_goto")
  bind(nil, "f1", "rexkit_keys")
  if app.settings.alt_shortcuts then
    for _, side in ipairs({ { "h", "left", "previous" }, { "l", "right", "next" } }) do
      local key, direction, tab = side[1], side[2], side[3]
      bind(nil, "alt+" .. key, action { name = "focus_" .. direction .. "_or_tab",
        title = "Focus " .. direction .. " or " .. tab .. " tab",
        run = function(ctx) return app.focus_or_tab(ctx, direction) end })
    end
    bind(nil, "alt+j", "pane.focus", { direction = "down", wrap = false })
    bind(nil, "alt+k", "pane.focus", { direction = "up", wrap = false })
    bind(nil, "alt+n", "pane.split", { direction = "auto" })
    bind(nil, "alt+i", "client.tab.move.backward")
    bind(nil, "alt+o", "client.tab.move.forward")
    bind(nil, "alt+=", grow)
    bind(nil, "alt+shift+=", grow)
    bind(nil, "alt+-", shrink)
  end
end
