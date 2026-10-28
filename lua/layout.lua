-- Layout commands Rex has no native action for: ratio-based grow/shrink, swap
-- with the next or previous pane, tab history (last tab), focus-or-adjacent-
-- tab, and moving a pane into a neighbouring tab. Plain focus, swap by
-- direction, splits and tab moves are bound to native client actions.
return function(app)
  local EPS = 1e-6

  -- resize moves the border on the `direction` side of the pane outwards
  -- (grow) or inwards (shrink); with no direction it moves the innermost one.
  function app.resize(ctx, direction, shrink)
    local _, source = app.current(ctx)
    local axis = (direction == "left" or direction == "right") and "horizontal" or "vertical"
    local side = (direction == "left" or direction == "up") and "after" or "before"
    for i = #source.parents, 1, -1 do
      local parent = source.parents[i]
      if not direction or (parent.split.direction == axis and parent.side == side) then
        local sign = (shrink and -1 or 1) * (parent.side == "before" and 1 or -1)
        local ratio = math.max(0.1, math.min(0.9, parent.split.ratio + sign * app.settings.resize_step))
        app.call("session.resize_pane", { session_id = ctx.session_id, direction = parent.split.direction,
          ratio = ratio, before_block_id = app.first_block(parent.split.before),
          after_block_id = app.first_block(parent.split.after) })
        return { resized = true, ratio = ratio }
      end
    end
    return { resized = false }
  end

  -- swap exchanges the pane with the one `step` places away in layout order
  -- (same layer, wrapping). The programs keep running.
  function app.swap(ctx, step)
    local view, source = app.current(ctx)
    local peers, index = {}, nil
    for _, item in ipairs(app.blocks(view)) do
      if item.layer.layer_id == source.layer.layer_id then
        peers[#peers + 1] = item
        if item.id == source.id then index = #peers end
      end
    end
    if #peers < 2 then return { moved = false } end
    local target = peers[(index - 1 + step) % #peers + 1]
    app.call("session.swap_blocks", { session_id = ctx.session_id, block_id = source.id, other_block_id = target.id })
    app.call("session.focus_block", { session_id = ctx.session_id, block_id = source.id })
    return { moved = true, swapped_with = target.id }
  end

  -- switch_tab shows tab `to` and records `from` as the last tab, in the
  -- session state so that ⌃T Tab still knows it after a config reload.
  -- (Tab switches outside rexkit's Lua, e.g. by mouse, are not recorded.)
  local function switch_tab(ctx, from, to)
    if not to or to.window_id == from.window_id then return { window_id = from.window_id } end
    app.select(ctx, to.window_id)
    app.session_state(ctx.session_id).last_tab = from.window_id
    app.save_state()
    return { window_id = to.window_id }
  end

  function app.tab_step(ctx, delta)
    local view, source = app.current(ctx)
    local _, i = app.find_window(view, source.window.window_id)
    local count = #view.windows
    return switch_tab(ctx, source.window, view.windows[(i - 1 + delta) % count + 1])
  end

  function app.tab_goto(ctx, index)
    local view, source = app.current(ctx)
    return switch_tab(ctx, source.window, view.windows[index])
  end

  -- last_tab toggles between the two most recent tabs this layer switched
  -- between (falls back to the previous tab).
  function app.last_tab(ctx)
    local view, source = app.current(ctx)
    local target = app.find_window(view, app.session_state(ctx.session_id).last_tab)
    if not target or target.window_id == source.window.window_id then return app.tab_step(ctx, -1) end
    return switch_tab(ctx, source.window, target)
  end

  -- focus_or_tab moves focus left/right, or to the adjacent tab when the pane
  -- already touches that edge of the window (Zellij's MoveFocusOrTab).
  function app.focus_or_tab(ctx, direction)
    local _, source = app.current(ctx)
    if app.is_user_float(source) then return app.focus_direction(ctx, direction) end
    local rect = source.rect
    local at_edge = source.layer.kind == "tiled"
      and ((direction == "left" and rect[1] <= EPS) or (direction == "right" and rect[1] + rect[3] >= 1 - EPS))
    if at_edge then return app.tab_step(ctx, direction == "left" and -1 or 1) end
    -- Rex's own neighbour search (it picks the pane that shares the most edge).
    app.call("session.focus_direction", { session_id = ctx.session_id, direction = direction, wrap = false })
    return { focused = direction }
  end

  -- anchor picks the block of a window to split beside: its focused tiled
  -- block, else its first tiled block.
  function app.anchor(view, window)
    local tiled = app.tiled_blocks(view, window.window_id)
    for _, item in ipairs(tiled) do
      if item.id == window.focused_block_id then return item end
    end
    return tiled[1]
  end

  -- break_pane moves the pane into the previous (-1) or next (1) tab, or
  -- into a new tab at either end.
  function app.break_pane(ctx, delta)
    local view, source = app.current(ctx)
    if source.layer.kind ~= "tiled" then app.fail("only panes in a tab can move to another tab") end
    local _, i = app.find_window(view, source.window.window_id)
    local target = view.windows[i + delta]
    local window_id
    if target then
      app.call("session.move_block", { session_id = ctx.session_id, block_id = source.id,
        anchor_block_id = app.anchor(view, target).id, direction = "horizontal", side = "after", ratio = 0.5 })
      window_id = target.window_id
    else
      window_id = app.move_to_new_tab(ctx.session_id, source.id, app.name(ctx.session_id, source))
      if delta < 0 then -- new tabs open at the end; this one belongs at the start
        rex.call("session.move_window", { session_id = ctx.session_id, window_id = window_id,
          before_window_id = view.windows[1].window_id })
      end
    end
    app.select(ctx, window_id, source.id)
    return { block_id = source.id, window_id = window_id }
  end
end
