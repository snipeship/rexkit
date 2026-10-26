-- Scratch: park a pane in a Scratch tab and bring it back to exactly where it
-- was. Programs are never restarted (blocks are moved or swapped). A tab whose
-- only pane is parked keeps a `rexkit hold` pane in its place.
--
-- State, per session, survives config reloads in workspace.json (core.lua's
-- app.session_state):
--   scratch   window id of the Scratch tab
--   previous  tab to return to from Scratch
--   parked    block id -> return address {window, label, name, next_windows,
--             sibling, sibling_blocks, direction, side, ratio, placeholder}
return function(app)
  local HOLD = "rexkit:hold"
  local session_state, save = app.session_state, app.save_state
  local busy = {} -- session id -> true while a Scratch command runs

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

  local function hold_layout(message, parked)
    local hints = {}
    -- gone: what the key does once the parked pane has exited (E on the
    -- hold pane then closes it).
    for _, hint in ipairs({ { "rexkit_scratch_park", "bring it back here", "close this pane" },
      { "rexkit_scratch", "visit Scratch" } }) do
      local chord = app.chord(hint[1])
      if chord then hints[#hints + 1] = { key = chord, label = hint[2], gone = hint[3] } end
    end
    local argv = { app.settings.binary, "hold", "--message", message, "--title", app.settings.scratch_label,
      "--since", tostring(os.time()), "--block", parked }
    if #hints > 0 then
      argv[#argv + 1] = "--hints"
      argv[#argv + 1] = rex.json.encode(hints)
    end
    return rex.layout.block { flavor = app.SHELL, label = HOLD,
      options = { shell = "none", login = false, command = argv, cwd = app.home } }
  end

  -- find_scratch adopts a tab named like Scratch only if no Scratch tab was
  -- ever recorded for the session; a recorded but closed one is recreated.
  local function find_scratch(view, st)
    if st.scratch then return app.find_window(view, st.scratch) end
    for _, window in ipairs(view.windows or {}) do
      if window.label == app.settings.scratch_label then
        st.scratch = window.window_id
        return window
      end
    end
  end

  local function in_scratch(item, st) return item ~= nil and item.window.window_id == st.scratch end

  -- return_tab is the tab to leave Scratch for: the tab Scratch was entered
  -- from if it still exists, otherwise the first tab that is not Scratch.
  local function return_tab(view, st)
    local window = app.find_window(view, st.previous)
    if window and window.window_id ~= st.scratch then return window end
    for _, w in ipairs(view.windows or {}) do
      if w.window_id ~= st.scratch then return w end
    end
    return nil
  end

  -- sweep drops records whose pane is gone (unless its hold pane remains, so
  -- E there can explain), and records of panes moved out of Scratch by hand
  -- together with their now misleading hold pane.
  local function sweep(sid, view, st)
    for id, record in pairs(st.parked) do
      local item = app.find_block(view, id)
      local hold = record.placeholder and app.find_block(view, record.placeholder)
      if item and not in_scratch(item, st) then
        st.parked[id] = nil
        if hold then app.close_block(sid, hold.id) end
      elseif not item and not hold then
        st.parked[id] = nil
      end
    end
  end

  -- largest returns the biggest tiled block of a window and the split
  -- direction that keeps its halves closest to square.
  local function largest(view, sid, window)
    local best, area = nil, -1
    for _, item in ipairs(app.tiled_blocks(view, window.window_id)) do
      local a = item.rect[3] * item.rect[4]
      if a > area then best, area = item, a end
    end
    local size = app.terminal(sid, best.id, "size")
    local wide = best.rect[3] >= best.rect[4]
    if size and size.columns and size.rows then wide = size.columns >= 2 * size.rows end -- cells are ~2:1
    return best, wide and "horizontal" or "vertical"
  end

  local function following_windows(view, window_id)
    local ids, found = {}, false
    for _, window in ipairs(view.windows or {}) do
      if found and #ids < 3 then ids[#ids + 1] = window.window_id end
      if window.window_id == window_id then found = true end
    end
    return ids
  end

  local function park(ctx, view, source, st)
    local sid = ctx.session_id
    if source.layer.kind ~= "tiled" then app.fail("move a floating pane into a tab before parking it") end
    local home = source.window
    local name = app.name(sid, source)
    local record = { window = home.window_id, label = home.label, name = name,
      next_windows = following_windows(view, home.window_id) }
    local parent = source.parents[#source.parents]
    if parent then
      local sibling = parent.split[parent.side == "before" and "after" or "before"]
      record.sibling, record.sibling_blocks = app.node_id(sibling), app.leaves(sibling)
      record.direction, record.side, record.ratio = parent.split.direction, parent.side, parent.split.ratio
    else -- the tab's only pane: keep the tab with a hold pane in its place
      local held = app.call("session.new_split", { session_id = sid, anchor_id = source.id,
        direction = "horizontal", side = "after", focus = false,
        layout = hold_layout(name .. " is parked in " .. app.settings.scratch_label, source.id) })
      record.placeholder = held.block_ids[1]
    end
    local scratch = find_scratch(view, st)
    if scratch then
      local anchor, direction = largest(view, sid, scratch)
      app.call("session.move_block", { session_id = sid, block_id = source.id, anchor_block_id = anchor.id,
        direction = direction, side = "after", ratio = 0.5 })
    else
      st.scratch = app.move_to_new_tab(sid, source.id, app.settings.scratch_label)
    end
    st.previous = home.window_id
    st.parked[source.id] = record
    save()
    app.select(ctx, st.scratch, source.id)
    app.queue(ctx, "pane.balance")
    return { parked = true, block_id = source.id, window_id = st.scratch }
  end

  -- put_back places the pane at its recorded address, trying in order: its
  -- hold pane, its old sibling block, its old sibling split, any surviving
  -- block of that sibling, the home tab, and finally a new tab where the
  -- home tab used to be. Returns the window it went to.
  local function put_back(sid, view, source, record)
    local home = app.find_window(view, record.window)
    local hold = record.placeholder and app.find_block(view, record.placeholder)
    if hold then
      app.call("session.swap_blocks", { session_id = sid, block_id = source.id, other_block_id = hold.id })
      app.close_block(sid, hold.id)
      return hold.window.window_id
    end
    if home and record.sibling and app.contains(home, record.sibling) then
      if record.sibling:match("^block:") then
        app.call("session.move_block", { session_id = sid, block_id = source.id, anchor_block_id = record.sibling,
          direction = record.direction, side = record.side, ratio = record.ratio })
      else -- recreate the split around the whole surviving sibling subtree
        local slot = app.call("session.new_split", { session_id = sid, anchor_id = record.sibling,
          direction = record.direction, side = record.side, ratio = record.ratio, focus = false,
          layout = app.slot_layout() }).block_ids[1]
        app.call("session.swap_blocks", { session_id = sid, block_id = source.id, other_block_id = slot })
        app.close_block(sid, slot)
      end
      return home.window_id
    end
    if home then
      local anchor = app.anchor(view, home).id
      for _, id in ipairs(record.sibling_blocks or {}) do
        local item = app.find_block(view, id)
        if item and item.window.window_id == home.window_id and item.layer.kind == "tiled" then
          anchor = id
          break
        end
      end
      app.call("session.move_block", { session_id = sid, block_id = source.id, anchor_block_id = anchor,
        direction = record.direction or "horizontal", side = record.side or "after", ratio = record.ratio or 0.5 })
      return home.window_id
    end
    local window_id = app.move_to_new_tab(sid, source.id, record.label or source.label or "Terminal")
    for _, id in ipairs(record.next_windows or {}) do
      if app.find_window(view, id) then
        rex.call("session.move_window", { session_id = sid, window_id = window_id, before_window_id = id })
        break
      end
    end
    return window_id
  end

  local function restore(ctx, view, source, record, st)
    local window_id = put_back(ctx.session_id, view, source, record)
    st.parked[source.id] = nil
    st.previous = window_id
    save()
    app.select(ctx, window_id, source.id)
    return { restored = true, block_id = source.id, window_id = window_id }
  end

  -- park_or_restore is Pane mode E.
  function app.park_or_restore(ctx)
    return guarded(ctx.session_id, function()
      local view, source = app.current(ctx)
      local sid = ctx.session_id
      local st = session_state(sid)
      sweep(sid, view, st)
      for id, record in pairs(st.parked) do
        if record.placeholder == source.id then -- E on a hold pane
          local parked = app.find_block(view, id)
          if parked then return restore(ctx, view, parked, record, st) end
          st.parked[id] = nil
          save()
          app.close_block(sid, source.id)
          app.notify(ctx, "The parked pane was closed; removed its placeholder", "warn")
          return { cleared = true }
        end
      end
      if source.label == HOLD then -- a hold pane whose record is gone
        app.close_block(sid, source.id)
        app.notify(ctx, "Nothing was parked here; removed the placeholder", "warn")
        return { cleared = true }
      end
      find_scratch(view, st)
      if in_scratch(source, st) then
        local record = st.parked[source.id]
        if record then return restore(ctx, view, source, record, st) end
        -- a pane started in Scratch joins the tab Scratch was opened from
        local home = return_tab(view, st)
        if not home then app.fail("there is no other tab to move this pane to") end
        return restore(ctx, view, source, { window = home.window_id }, st)
      end
      return park(ctx, view, source, st)
    end)
  end

  -- toggle_scratch is Pane mode W: visit Scratch, or go back from it.
  function app.toggle_scratch(ctx)
    return guarded(ctx.session_id, function()
      local view, source = app.current(ctx)
      local sid = ctx.session_id
      local st = session_state(sid)
      local scratch = find_scratch(view, st)
      local target
      if scratch and source.window.window_id == scratch.window_id then
        target = return_tab(view, st)
      else
        st.previous = source.window.window_id
        if not scratch then
          local created = app.call("session.new_window", { session_id = sid,
            window_label = app.settings.scratch_label, focus = false,
            layout = app.shell_layout(app.cwd(sid, source.id)) })
          st.scratch = created.window_id
        end
        target = { window_id = st.scratch }
      end
      save()
      if target then app.select(ctx, target.window_id) end
      return { window_id = target and target.window_id, scratch_window_id = st.scratch }
    end)
  end
end
