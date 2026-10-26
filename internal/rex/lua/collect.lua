-- Collects every session's view plus, for each terminal block, its process,
-- program status records and title, in one connection.
-- A session must be attached before it can be viewed; attach returns the view.
-- A failed session.list fails the whole script (an empty inventory would read
-- as "nothing is running"); a session that cannot be attached is reported in
-- errors and skipped.
local TERMINAL = "com.superlogical.terminal"
local out = {sessions = {}, errors = {}}

local function block_method(sid, bid, name)
  local res = rex.call("block.method", {session_id = sid, block_id = bid, method = name, args = {}})
  return res
end

local list, err = rex.call("session.list", {})
if not list then error(tostring(err or "session.list returned nothing"), 0) end

for order, s in ipairs(list.sessions or {}) do
  local attached, aerr = rex.call("session.attach", {session_id = s.session_id})
  if not attached or not attached.view then
    out.errors[#out.errors + 1] = {session_id = s.session_id, error = tostring(aerr or "session.attach returned no view")}
  else
    local view = attached.view
    view.order = order - 1
    for _, w in ipairs(view.windows or {}) do
      for _, l in ipairs(w.layers or {}) do
        for _, b in ipairs(l.blocks or {}) do
          if b.creator_name == TERMINAL then
            local status = block_method(s.session_id, b.block_id, "program_status")
            local title = block_method(s.session_id, b.block_id, "title")
            b.terminal = {
              process = block_method(s.session_id, b.block_id, "process"),
              status = status and status.records or {},
              title = title and title.title or nil,
            }
          end
        end
      end
    end
    out.sessions[#out.sessions + 1] = view
  end
end
return out
