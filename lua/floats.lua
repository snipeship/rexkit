-- Floating panes, as in Zellij: ⌃P w hides the tab's floating panes, shows
-- them again where they were, or opens one; ⌃P e floats the pane or puts a
-- floating pane back in the layout; ⌃P n in a floating pane opens another.
-- rexkit's own floats (tools and toasts, labelled rexkit:*) are never the
-- user's floating panes; floats made with Rex's own Float Pane are.
--
-- Hiding detaches each block (session.detach_block): its program keeps
-- running, and Rex closes it when the program ends. Showing places each
-- one again in a new layer at its old bounds: a short-lived slot opens the
-- layer, the block moves in beside it and the slot closes, so neither the
-- layout under it nor the pane's own size jumps on the way.
--
-- State, per session, survives config reloads in workspace.json (core.lua's
-- app.session_state):
--   hidden  window id -> {layers = {{bounds, tree}, back to front}, focus}:
--           a tree is a block id or {direction, ratio, before, after}
--   homes   block id -> where ⌃P e floated it from {window, sibling,
--           sibling_blocks, direction, side, ratio}
--   back    window id -> the tiled pane that had focus before rexkit gave it
--           to a floating pane
--   zoom    window id -> that pane, when it was zoomed: focus on a floating
--           pane ends a zoom, and hiding the floats zooms it again
return function(app)
  local session_state, save = app.session_state, app.save_state
  local busy = {} -- session id -> true while a floats command runs
  local EPS = 1e-6
  -- New floating panes: half the window, centred, each new one a little
  -- down and to the right of one already there, as far as the window goes.
  local SIZE, STEP_X, STEP_Y = 0.5, 0.02, 0.04

  -- guarded runs fn once per session at a time, so a double press can't act
  -- on a stale view (Rex calls may yield to other actions).
  local function guarded(sid, fn)
    if busy[sid] then return { busy = true } end
    busy[sid] = true
    local ok, result = pcall(fn)
    busy[sid] = nil
    if not ok then error(result, 0) end
    return result
  end

  local function own(label) return (label or ""):match("^rexkit:") ~= nil end

  -- user_layers lists a window's floating layers that hold the user's
  -- panes, back to front.
  local function user_layers(window)
    local list = {}
    for _, layer in ipairs(window.layers or {}) do
      local mine = layer.kind == "floating" and #(layer.blocks or {}) > 0
      for _, block in ipairs(layer.blocks or {}) do
        if own(block.label) then mine = false end
      end
      if mine then list[#list + 1] = layer end
    end
    return list
  end

  function app.is_user_float(item)
    return item ~= nil and item.layer.kind == "floating" and not own(item.label)
  end

  local function tree_of(node)
    if node.block_id then return node.block_id end
    local split = node.split
    return { direction = split.direction, ratio = split.ratio, before = tree_of(split.before),
      after = tree_of(split.after) }
  end

  local function leaves(tree, list)
    list = list or {}
    if type(tree) == "string" then
      list[#list + 1] = tree
    elseif type(tree) == "table" then
      leaves(tree.before, list)
      leaves(tree.after, list)
    end
    return list
  end

  -- prune drops the blocks that are not in keep from a tree, a split
  -- whose side is gone giving way to the other side.
  local function prune(tree, keep)
    if type(tree) == "string" then return keep[tree] and tree or nil end
    local before, after = prune(tree.before, keep), prune(tree.after, keep)
    if before and after then
      return { direction = tree.direction, ratio = tree.ratio, before = before, after = after }
    end
    return before or after
  end

  -- detached is the set of the session's blocks that are in no window.
  local function detached(sid)
    local set = {}
    for _, block in ipairs((rex.call("session.list_blocks", { session_id = sid }) or {}).blocks or {}) do
      if block.placement ~= "placed" then set[block.block_id] = true end
    end
    return set
  end

  -- still_hidden drops from a tab's hidden entry the panes that are no
  -- longer detached (placed again, or closed when their program ended).
  -- Returns the entry, or nil when nothing of it is left.
  local function still_hidden(entry, loose)
    local layers = {}
    for _, layer in ipairs(entry.layers) do
      local tree = prune(layer.tree, loose)
      if tree then layers[#layers + 1] = { bounds = layer.bounds, tree = tree } end
    end
    entry.layers = layers
    if entry.focus and not loose[entry.focus] then entry.focus = nil end
    return #layers > 0 and entry or nil
  end

  -- sweep forgets what is gone: hidden panes whose program ended, homes of
  -- closed panes, and the hidden panes of a closed tab, which it closes
  -- (nothing would ever show them again).
  local function sweep(sid, view, st)
    local loose = detached(sid)
    for window_id, entry in pairs(st.hidden) do
      if app.find_window(view, window_id) then
        st.hidden[window_id] = still_hidden(entry, loose)
      else
        for _, layer in ipairs(entry.layers) do
          for _, id in ipairs(leaves(layer.tree)) do
            if loose[id] then app.close_block(sid, id) end
          end
        end
        st.hidden[window_id] = nil
      end
    end
    for id in pairs(st.homes) do -- a hidden pane keeps its home
      if not (app.find_block(view, id) or loose[id]) then st.homes[id] = nil end
    end
    for _, map in ipairs({ st.back, st.zoom }) do
      for window_id, id in pairs(map) do
        local item = app.find_block(view, id)
        if not (item and item.window.window_id == window_id and item.layer.kind == "tiled") then map[window_id] = nil end
      end
    end
  end

  -- place returns the bounds of a new floating pane in window: centred, or
  -- a step down and to the right of each user float already at the spot.
  local function place(window)
    local taken = {}
    for _, layer in ipairs(user_layers(window)) do taken[#taken + 1] = layer.bounds end
    local x0 = (1 - SIZE) / 2
    for step = 0, math.huge do
      local x, y = x0 + step * STEP_X, x0 + step * STEP_Y
      if x + SIZE > 1 + EPS or y + SIZE > 1 + EPS then break end -- Rex takes only bounds inside the window
      local free = true
      for _, b in ipairs(taken) do
        if math.abs(b[1] - x) < STEP_X / 2 and math.abs(b[2] - y) < STEP_Y / 2 then free = false end
      end
      if free then return { x, y, SIZE, SIZE } end
    end
    return { x0, x0, SIZE, SIZE }
  end

  -- remember_back notes window's focused pane, if it is a tiled one, as
  -- the pane focus goes back to when the floats go, and whether it is
  -- zoomed.
  local function remember_back(st, view, window)
    local item = app.find_block(view, window.focused_block_id)
    if item and item.layer.kind == "tiled" then
      st.back[window.window_id] = item.id
      st.zoom[window.window_id] = window.zoomed_block_id == item.id and item.id or nil
    end
  end

  -- hide detaches every user float of window and returns how many panes
  -- it hid. Focus goes back to the tiled pane it came from, when known,
  -- zoomed again if it was zoomed (as in Zellij, where floating panes
  -- show over a fullscreen pane and leave it fullscreen).
  local function hide(sid, view, window, st)
    local layers = user_layers(window)
    if #app.tiled_blocks(view, window.window_id) == 0 then
      app.fail("this tab has only floating panes: put one in the layout first, or the tab would close")
    end
    local entry = st.hidden[window.window_id] or { layers = {} }
    local focused = window.focused_block_id
    local was = app.find_block(view, focused)
    local count = 0
    for _, layer in ipairs(layers) do
      local tree = tree_of(layer.layout)
      entry.layers[#entry.layers + 1] = { bounds = layer.bounds, tree = tree }
      for _, id in ipairs(leaves(tree)) do
        if id == focused then entry.focus = id end
        count = count + 1
      end
    end
    if not entry.focus then
      local front = entry.layers[#entry.layers]
      entry.focus = front and leaves(front.tree)[1]
    end
    st.hidden[window.window_id] = entry
    local zoomed = st.zoom[window.window_id]
    st.zoom[window.window_id] = nil
    save() -- before the first detach: a pane detached and not recorded would be lost
    for _, layer in ipairs(layers) do
      for _, id in ipairs(leaves(tree_of(layer.layout))) do
        app.call("session.detach_block", { session_id = sid, block_id = id })
      end
    end
    local target = was and was.id
    local back = app.find_block(view, st.back[window.window_id])
    if was and was.layer.kind == "floating" and back and back.layer.kind == "tiled" then target = back.id end
    if target and target == zoomed and (window.zoomed_block_id or "") == "" then
      -- zooming focuses it too
      app.call("session.zoom_block", { session_id = sid, block_id = target, zoom = true })
    elseif target and target ~= (was and was.id) then
      app.call("session.focus_block", { session_id = sid, block_id = target })
    end
    return count
  end

  -- rebuild places the blocks of tree, whose first block is already placed
  -- where the whole tree goes: each split is made by moving the first
  -- block of its after side beside the first block of its before side.
  local function rebuild(sid, tree)
    if type(tree) ~= "table" then return end
    app.call("session.move_block", { session_id = sid, block_id = leaves(tree.after)[1],
      anchor_block_id = leaves(tree.before)[1], direction = tree.direction, side = "after", ratio = tree.ratio })
    rebuild(sid, tree.before)
    rebuild(sid, tree.after)
  end

  -- place_layer places a hidden layer again at its bounds, and returns its
  -- blocks. A pane whose program has ended since is left out.
  local function place_layer(sid, window_id, layer)
    local tree = prune(layer.tree, detached(sid))
    if not tree then return {} end
    local slot = app.call("session.new_layer", { session_id = sid, window_id = window_id,
      bounds = layer.bounds, focus = false, layout = app.slot_layout() }).block_ids[1]
    local ok, err = pcall(app.call, "session.move_block", { session_id = sid, block_id = leaves(tree)[1],
      anchor_block_id = slot, direction = "horizontal", side = "after", ratio = 0.001 })
    app.close_block(sid, slot)
    if not ok then error(err, 0) end
    rebuild(sid, tree)
    return leaves(tree)
  end

  -- show places window's hidden panes again, back to front, and focuses
  -- the one that had focus (focus = false leaves focus alone). Returns the
  -- blocks it showed.
  local function show(sid, view, window, st, focus)
    local entry = st.hidden[window.window_id]
    local shown = {}
    if not entry then return shown end
    remember_back(st, view, window)
    -- A layer leaves the record only once it is placed: a pane detached and
    -- not recorded would be lost. After a failure, what is still detached
    -- stays recorded.
    local ok, err = pcall(function()
      while entry.layers[1] do
        for _, id in ipairs(place_layer(sid, window.window_id, entry.layers[1])) do shown[#shown + 1] = id end
        table.remove(entry.layers, 1)
        if not entry.layers[1] then st.hidden[window.window_id] = nil end
        save()
      end
    end)
    if not ok then
      st.hidden[window.window_id] = still_hidden(entry, detached(sid))
      save()
      error(err, 0)
    end
    -- rexkit's own floats (a toast, a tool) stay in front
    local now = app.find_window(app.view(sid), window.window_id)
    for _, layer in ipairs(now and now.layers or {}) do
      if layer.kind == "floating" and layer.blocks[1] and own(layer.blocks[1].label) then
        rex.call("session.raise_layer", { session_id = sid, layer_id = layer.layer_id })
      end
    end
    local target = shown[#shown]
    for _, id in ipairs(shown) do
      if id == entry.focus then target = id end
    end
    if focus ~= false and target then app.call("session.focus_block", { session_id = sid, block_id = target }) end
    return shown
  end

  -- open_float opens a new floating shell pane in window, in the folder of
  -- the pane `from`, and focuses it.
  local function open_float(sid, view, window, from, st)
    remember_back(st, view, window)
    local created = app.call("session.new_layer", { session_id = sid, window_id = window.window_id,
      bounds = place(window), focus = true, layout = app.shell_layout(app.cwd(sid, from)) })
    save()
    return created.block_ids[1]
  end

  -- say_hidden toasts how many floating panes hide just hid.
  local function say_hidden(ctx, count)
    local chord = app.chord("rexkit_floats")
    local what = count == 1 and "1 floating pane" or count .. " floating panes"
    app.notify(ctx, what .. " hidden" .. (chord and (" · {key:" .. chord .. "} shows them") or ""), "info")
  end

  -- toggle_floats is Pane mode w.
  function app.toggle_floats(ctx)
    return guarded(ctx.session_id, function()
      local view, source = app.current(ctx)
      local sid, window = ctx.session_id, source.window
      if own(source.label) then app.fail("close the open tool first") end
      local st = session_state(sid)
      sweep(sid, view, st)
      if #user_layers(window) > 0 then
        local count = hide(sid, view, window, st)
        say_hidden(ctx, count)
        return { hidden = count }
      end
      if st.hidden[window.window_id] then
        return { shown = #show(sid, view, window, st) }
      end
      return { opened = open_float(sid, view, window, source.id, st) }
    end)
  end

  -- new_pane is Pane mode n: a new floating pane from a floating pane, as
  -- in Zellij; a split from any other pane.
  function app.new_pane(ctx)
    local view, source = app.current(ctx)
    if not app.is_user_float(source) then
      app.queue(ctx, "pane.split", { direction = "auto" })
      return { split = true }
    end
    return guarded(ctx.session_id, function()
      local st = session_state(ctx.session_id)
      sweep(ctx.session_id, view, st)
      return { opened = open_float(ctx.session_id, view, source.window, source.id, st) }
    end)
  end

  -- float floats a tiled pane, remembering where it was in the layout.
  local function float(sid, view, source, st)
    local window = source.window
    if #app.tiled_blocks(view, window.window_id) == 1 then -- Rex can't put a pane back in an empty layout
      app.fail("this is the last pane in the tab's layout: split it first")
    end
    local home = { window = window.window_id }
    local parent = source.parents[#source.parents]
    if parent then
      local sibling = parent.split[parent.side == "before" and "after" or "before"]
      home.sibling, home.sibling_blocks = app.node_id(sibling), app.leaves(sibling)
      home.direction, home.side, home.ratio = parent.split.direction, parent.side, parent.split.ratio
    end
    if st.hidden[window.window_id] then -- the tab's floating panes come into view with it
      show(sid, view, window, st, false)
      window = app.find_window(app.view(sid), window.window_id)
    end
    st.homes[source.id] = home
    st.back[window.window_id] = home.sibling_blocks and home.sibling_blocks[1]
    st.zoom[window.window_id] = nil -- a zoomed pane that floats is no longer zoomed
    save()
    app.call("session.float_block", { session_id = sid, block_id = source.id, bounds = place(window) })
    app.call("session.focus_block", { session_id = sid, block_id = source.id })
    return { floated = source.id }
  end

  -- overlap is how much of a float's rect a tiled pane's rect covers.
  local function overlap(a, b)
    local w = math.min(a[1] + a[3], b[1] + b[3]) - math.max(a[1], b[1])
    local h = math.min(a[2] + a[4], b[2] + b[4]) - math.max(a[2], b[2])
    return (w > 0 and h > 0) and w * h or 0
  end

  -- beside picks the tiled pane a float goes back beside: the one that had
  -- focus before the floats took it, else the one under most of it.
  local function beside(view, source, st)
    local tiled = app.tiled_blocks(view, source.window.window_id)
    local back = st.back[source.window.window_id]
    for _, item in ipairs(tiled) do
      if item.id == back then return item end
    end
    local best, most = tiled[1], -1
    for _, item in ipairs(tiled) do
      local o = overlap(item.rect, source.rect)
      if o > most + EPS then best, most = item, o end
    end
    return best
  end

  -- in_layout reports whether a block or split id is in window's layout,
  -- not in one of its floating layers.
  local function in_layout(window, id)
    for _, layer in ipairs(window.layers or {}) do
      if layer.kind == "tiled" and app.contains({ layers = { layer } }, id) then return true end
    end
    return false
  end

  -- embed puts a floating pane back in the layout: where ⌃P e floated it
  -- from, if that place is still in the layout, else beside the pane
  -- beside() picks. Rex has no way to put a block in an empty layout (a new
  -- split goes beside the focused block, here a floating one). As in
  -- Zellij, the tab's other floating panes then hide.
  local function embed(ctx, sid, view, source, st)
    local window = source.window
    local home = st.homes[source.id]
    if home and home.window == window.window_id and home.sibling and in_layout(window, home.sibling) then
      if home.sibling:match("^block:") then
        app.call("session.move_block", { session_id = sid, block_id = source.id, anchor_block_id = home.sibling,
          direction = home.direction, side = home.side, ratio = home.ratio })
      else -- the split around the whole sibling subtree, made again
        local slot = app.call("session.new_split", { session_id = sid, anchor_id = home.sibling,
          direction = home.direction, side = home.side, ratio = home.ratio, focus = false,
          layout = app.slot_layout() }).block_ids[1]
        app.call("session.swap_blocks", { session_id = sid, block_id = source.id, other_block_id = slot })
        app.close_block(sid, slot)
      end
    else
      local anchor = beside(view, source, st)
      if not anchor then app.fail("this tab's layout is empty, so there is nowhere to put the pane") end
      local size = app.terminal(sid, anchor.id, "size")
      local wide = anchor.rect[3] >= anchor.rect[4]
      if size and size.columns and size.rows then wide = size.columns >= 2 * size.rows end -- cells are ~2:1
      app.call("session.move_block", { session_id = sid, block_id = source.id, anchor_block_id = anchor.id,
        direction = wide and "horizontal" or "vertical", side = "after", ratio = 0.5 })
    end
    st.homes[source.id] = nil
    save()
    app.call("session.focus_block", { session_id = sid, block_id = source.id })
    local result = { embedded = source.id }
    local now = app.view(sid)
    window = app.find_window(now, window.window_id)
    if window and #user_layers(window) > 0 then
      result.hidden = hide(sid, now, window, st)
      say_hidden(ctx, result.hidden)
    end
    return result
  end

  -- toggle_float is Pane mode e.
  function app.toggle_float(ctx)
    return guarded(ctx.session_id, function()
      local view, source = app.current(ctx)
      local sid = ctx.session_id
      if own(source.label) then app.fail("close the open tool first") end
      local st = session_state(sid)
      sweep(sid, view, st)
      if source.layer.kind == "floating" then return embed(ctx, sid, view, source, st) end
      return float(sid, view, source, st)
    end)
  end

  -- focus_direction is Pane mode h/j/k/l. From a floating pane, as in
  -- Zellij, focus moves to the nearest of the tab's floating panes that way
  -- and brings it to the front. Floats overlap, so "that way" goes by their
  -- centres: Rex's pane.focus only finds panes whose edges clear the focused
  -- one, which cascaded floats never do.
  local AXES = { left = { 1, -1 }, right = { 1, 1 }, up = { 2, -1 }, down = { 2, 1 } }
  local function centre(r) return { r[1] + r[3] / 2, r[2] + r[4] / 2 } end

  function app.focus_direction(ctx, direction)
    local view, source = app.current(ctx)
    if not app.is_user_float(source) then
      app.queue(ctx, "pane.focus", { direction = direction, wrap = false })
      return { focused = direction }
    end
    local axis, sign = AXES[direction][1], AXES[direction][2]
    local from, best, best_score = centre(source.rect), nil, math.huge
    for _, item in ipairs(app.blocks(view)) do
      if item.window.window_id == source.window.window_id and item.id ~= source.id and app.is_user_float(item) then
        local to = centre(item.rect)
        local ahead = (to[axis] - from[axis]) * sign
        -- straight ahead wins over nearer but off to the side
        local score = ahead + 2 * math.abs(to[3 - axis] - from[3 - axis])
        if ahead > EPS and score < best_score then best, best_score = item, score end
      end
    end
    if not best then return { focused = false } end
    local layers = user_layers(source.window)
    local front = layers[#layers]
    if front and front.layer_id ~= best.layer.layer_id then
      -- just above the other floating panes: rexkit's own floats stay in front
      app.call("session.raise_layer", { session_id = ctx.session_id, layer_id = best.layer.layer_id,
        relative_layer_id = front.layer_id })
    end
    app.call("session.focus_block", { session_id = ctx.session_id, block_id = best.id })
    return { focused = best.id }
  end

  -- show_floats shows the hidden panes of a tab without focus moving (Go to
  -- shows them to go to one of them, and focuses it itself).
  function app.show_floats(ctx, args)
    local sid = args.session_id or ctx.session_id
    return guarded(sid, function()
      local view = app.view(sid)
      local window = app.find_window(view, args.window_id)
      if not window then app.fail("that tab is closed") end
      local st = session_state(sid)
      sweep(sid, view, st)
      return { shown = show(sid, view, window, st, false) }
    end)
  end

  -- show_hidden shows the hidden panes of every tab, in every session, with
  -- focus left alone, and closes those of closed tabs: `rexkit uninstall`
  -- leaves nothing running that only rexkit could show. Returns how many
  -- panes it showed.
  function app.show_hidden()
    local count = 0
    for _, sid in ipairs(app.state_sessions()) do
      if next(session_state(sid).hidden) then
        local shown = guarded(sid, function()
          local view = rex.call("session.view", { session_id = sid })
          if not view then return 0 end -- the session is gone, and its panes with it
          local st = session_state(sid)
          sweep(sid, view, st)
          save()
          local tabs, n = {}, 0
          for window_id in pairs(st.hidden) do tabs[#tabs + 1] = window_id end
          for _, window_id in ipairs(tabs) do
            n = n + #show(sid, view, app.find_window(view, window_id), st, false)
          end
          return n
        end)
        if type(shown) == "number" then count = count + shown end
      end
    end
    return { shown = count }
  end

  -- reveal_float shows the hidden panes of a tab when block is one of them,
  -- leaving focus to the caller (Go to's result, tools.lua).
  function app.reveal_float(sid, window_id, block)
    local entry = session_state(sid).hidden[window_id or ""]
    for _, layer in ipairs(entry and entry.layers or {}) do
      for _, id in ipairs(leaves(layer.tree)) do
        if id == block then return pcall(app.show_floats, {}, { session_id = sid, window_id = window_id }) end
      end
    end
  end

  -- sweep_floats closes the hidden panes of tabs that closed (the agent
  -- watcher runs it in a session where a hidden pane's tab is gone; the
  -- config cannot, as it loads).
  function app.sweep_floats(ctx, args)
    local sid = args.session_id or ctx.session_id
    if not sid then return { swept = false } end
    return guarded(sid, function()
      local view = rex.call("session.view", { session_id = sid })
      if not view then return { swept = false } end
      local st = session_state(sid)
      sweep(sid, view, st)
      save()
      return { swept = true }
    end)
  end
end
