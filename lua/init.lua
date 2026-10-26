-- rexkit's Rex config layer. ~/.config/rex/init.lua loads it with dofile.
-- It reads the settings, loads each module in isolation (one broken module
-- doesn't take the others down) and returns the app table.
local lua_dir = debug.getinfo(1, "S").source:gsub("^@", ""):match("^(.*)/[^/]*$") or "."
local root = lua_dir:match("^(.*)/[^/]*$") or "."
local home = os.getenv("HOME") or "/"

-- xdg mirrors internal/state: an absolute $XDG_* wins, else a home default.
local function xdg(env, fallback)
  local value = os.getenv(env)
  if value and value:sub(1, 1) == "/" then return value .. "/rexkit" end
  return home .. "/" .. fallback .. "/rexkit"
end

local app = {
  root = root,
  home = home,
  warnings = {},
  loaded = {},
  failed = {}, -- "<module>.lua failed to load: <why>", one per failed module
  paths = {
    config = xdg("XDG_CONFIG_HOME", ".config"),
    state = xdg("XDG_STATE_HOME", ".local/state"),
  },
}
app.paths.settings = app.paths.config .. "/settings.lua"

function app.warn(message)
  app.warnings[#app.warnings + 1] = message
  rex.log("warn", "rexkit: " .. message)
end

local function is_list_of_strings(value)
  if #value == 0 then return false end
  for _, item in pairs(value) do
    if type(item) ~= "string" then return false end
  end
  return true
end

local function invalid(entry, value)
  local name, default = entry[1], entry[2]
  if type(value) ~= type(default) then
    return ("%s should be a %s, not a %s"):format(name, type(default), type(value))
  end
  if type(default) == "table" and not is_list_of_strings(value) then
    return name .. " should be a non-empty list of strings"
  end
  if entry.choices then
    for _, choice in ipairs(entry.choices) do
      if value == choice then return nil end
    end
    return ("%s should be one of %s"):format(name, table.concat(entry.choices, ", "))
  end
  if name == "resize_step" and not (value > 0 and value < 0.5) then -- not NaN either
    return "resize_step should be between 0 and 0.5"
  end
end

local function exists(path)
  local file = io.open(path, "r")
  if not file then return false end
  file:close()
  return true
end

-- expand resolves a leading ~/ against $HOME.
local function expand(path)
  if path:sub(1, 2) == "~/" then return home .. path:sub(2) end
  return path
end

-- check_programs warns about a shell or editor that isn't there: Rex starts
-- such a pane and it vanishes without a word. A missing shell falls back to
-- $SHELL (or /bin/zsh), so new panes, Scratch and Files terminals still open.
local function check_programs(settings)
  if settings.shell ~= "none" and not exists(settings.shell) then
    local login = os.getenv("SHELL")
    local fallback = (login and login:sub(1, 1) == "/" and exists(login)) and login or "/bin/zsh"
    app.warn(("shell %s is missing; using %s (install it, or set shell in %s)"):format(
      settings.shell == "" and '""' or settings.shell, fallback, app.paths.settings))
    settings.shell = fallback
  end
  local editor = settings.editor[1]
  if editor:sub(1, 1) == "/" and not exists(editor) then
    app.warn(("editor %s is missing; rexkit settings and Files' editor will not open (set editor in %s)"):format(
      editor, app.paths.settings))
  end
end

local function user_settings()
  local chunk, err = loadfile(app.paths.settings)
  if not chunk then
    local file = io.open(app.paths.settings, "r")
    if file then -- present but broken; a missing file just means defaults
      file:close()
      app.warn("settings.lua: " .. tostring(err))
    end
    return {}
  end
  local ok, values = pcall(chunk)
  if not ok then
    app.warn("settings.lua failed: " .. tostring(values))
    return {}
  end
  if type(values) ~= "table" then
    app.warn("settings.lua must return a table")
    return {}
  end
  return values
end

local function load_settings()
  app.defaults = dofile(lua_dir .. "/defaults.lua")
  local settings, known = {}, {}
  for _, entry in ipairs(app.defaults) do
    settings[entry[1]], known[entry[1]] = entry[2], entry
  end
  for name, value in pairs(user_settings()) do
    local entry = known[name]
    local problem = entry and invalid(entry, value)
    if not entry then
      app.warn(("unknown setting %q is ignored"):format(tostring(name)))
    elseif problem then
      app.warn(problem .. "; using the default")
    else
      settings[name] = value
    end
  end
  for _, name in ipairs({ "shell", "binary" }) do settings[name] = expand(settings[name]) end
  settings.editor[1] = expand(settings.editor[1])
  if settings.binary:sub(1, 1) ~= "/" then settings.binary = root .. "/" .. settings.binary end
  check_programs(settings)
  return settings
end

app.settings = load_settings()

-- Order matters: core first, modes last (locked mode passes every global
-- key through, so it needs the final list of global bindings).
for _, name in ipairs({ "core", "layout", "workspace", "tools", "bindings", "modes" }) do
  local ok, err = pcall(function() dofile(lua_dir .. "/" .. name .. ".lua")(app) end)
  if ok then
    app.loaded[#app.loaded + 1] = name
  else
    local why = name .. ".lua failed to load: " .. tostring(err)
    app.failed[#app.failed + 1] = why
    app.warn(why)
  end
end

-- `rexkit install` and `rexkit doctor` check the config with
-- `rexkit_strict = true` ahead of the loader, so a broken module fails the
-- check instead of loading a half-working layer. A normal load stays lenient.
if rawget(_G, "rexkit_strict") and #app.failed > 0 then
  error("rexkit: " .. table.concat(app.failed, "; "), 0)
end

return app
