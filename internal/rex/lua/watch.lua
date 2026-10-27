-- Streams change events as newline-delimited JSON until stopped.
-- Only events that change what Go to and the attention watcher show, and
-- config reloads (keymap_changed): no terminal.title_changed, since programs
-- that animate their title would re-collect the catalog every second.
local events = {
  "session_created", "session_destroyed", "session_label_changed", "session_view_changed",
  "window_created", "window_closed", "window_label_changed", "active_window_changed",
  "block_created", "block_closed",
  "terminal.program_status_changed", "terminal.program_status_removed",
  "terminal.pwd_changed", "terminal.process_changed", "terminal.child_exited",
  "keymap_changed",
}
for _, name in ipairs(events) do
  rex.on(name, function(ev)
    print(rex.json.encode({type = name, session_id = ev.session_id, block_id = ev.block_id}))
  end)
end
-- A tick now and then: once the reader is gone, printing it fails and the
-- script ends, so a watcher that was killed leaves no script behind.
rex.every(30, function() print(rex.json.encode({type = "tick"})) end)
print(rex.json.encode({type = "ready"}))
