-- Returns about the last `max` bytes of a terminal's formatted content.
-- Args: session, block, format, trim, unwrap, max.
-- Run it attached to the block's session (rex do -s SESSION).
--
-- Rex's format has no tail option: it formats the whole scrollback, returns
-- the first max_bytes inline and spools the rest as a blob. Asking for one
-- inline byte keeps the head of the history (which is thrown away) off the
-- wire; the tail is then read from the blob.
local a = rex.args
local function method(name, args)
  return rex.call("block.method", {session_id = a.session, block_id = a.block, method = name, args = args})
end

local first, err = method("format", {format = a.format, trim = a.trim, unwrap = a.unwrap, max_bytes = 1})
if not first then return {error = tostring(err)} end
if not first.id then return {content = first.content} end

local total = first.total or 0
local offset = math.max(0, total - a.max)
local parts = {}
for _ = 1, 64 do
  local blob, berr = method("blob_read", {id = first.id, offset = offset, max_bytes = 983040})
  if not blob then return {error = tostring(berr)} end
  parts[#parts + 1] = blob.data
  offset = offset + #blob.data
  if blob.eof or #blob.data == 0 then break end
end
return {content = table.concat(parts), partial = total > a.max}
