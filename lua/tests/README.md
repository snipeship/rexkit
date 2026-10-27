# Live checks for the Lua layer

`check.lua` drives a real Rex server through the rexkit config: keymap and
modes, locked pass-through, native pass-through of ⏎/⎋/⌃C, layout commands,
every Scratch edge case, and the tool float / handshake / dismiss / miss /
recovery paths. It creates and destroys sessions, so it **must run against an
isolated server**. It refuses to run unless you pass `"isolated":true`, and it
also refuses if the server has a session whose label doesn't start with
`rexkit-test`.

Never point it at your everyday server. Never put `lua/init.lua` into
`~/.config/rex` to test it.

## 1. Build the stand-in tool

The checks don't need the real tools. `standin/` is a small Go program (darwin
only) that imitates `rexkit`'s side of the tool handshake (see the README's
Architecture section):

| argv | behaviour |
|---|---|
| `<tool> --session S --origin-block B --origin-window W --state DIR [--zoomed] …` | Appends its argv as JSON to `$STANDIN_LOG`. Writes `DIR/owner.json` (with the `server` it talks to), prints `standin <tool> ready`, then reads keys in raw mode. |
| ⏎ (`\r`) | Takes the commit marker `DIR/commit` and reports in the result whether it was there (`"commit":true`). Writes a `goto` result for the origin. It writes `stay` instead if you typed `stay` first. Typing `goto S W B` first overrides the target, and `invoke X` adds `"invoke":"X"`. Then it closes its own layer, re-zooming the origin if it got `--zoomed`. |
| ⎋ (`\x1b`) | Writes `{"action":"close"}` and closes. It writes `stay` instead if you typed `back` first. |
| ⌃C (`\x03`) | Writes `{"action":"close"}` and closes. |
| `hints`: ⎋ and a letter in one write | ⌥ + a letter that the hints mode carries to the tool (`rexkit_hints_key`). Logs `["read","<letter>"]` and stays open. An ⎋ that comes in a read of its own closes the tool, as above. |
| `search`: F1 (`\eOP`) in one write | F1, which `rexkit_keys` carries into Search. Logs `["read","f1"]` and stays open. |
| `jump S W B LINE` typed before ⏎ | Search's ⏎ on a match: writes a page (`a needle`) into a new folder beside its state dir, and a `goto` result for pane B that invokes `rexkit_search_jump` with it, LINE and the query `needle`. |
| `slow` typed before ⏎, ⎋ or ⌃C | Answers that key 300 ms late (then as if `slow` hadn't been typed). |
| `await --state DIR [--timeout D]` | Polls `DIR/result.json` (1.5 s by default, as `rexkit await`). Prints it, or exits 3 on timeout. |
| `hold …` | Logs its argv, then idles (it ignores SIGINT). |
| `toast …` | Logs its argv and exits. |
| `attend --token T …` | Logs its argv and exits (the agent watcher; the Go integration tests run the real one). |
| `attend next …`, `attend back …` | Logs its argv and prints `jump.json` from the folder of `$STANDIN_LOG`, or a "No agent needs you" jump without one. |

```sh
go build -C lua/tests/standin -o "$T/standin/rexkit" .
```

`standin/` has its own `go.mod`, so `go build ./...` and `go test ./...` in the
main module skip it.

The stand-in makes its own `rex api call`s (to close its float, re-zoom the
origin) with `-S $REX_SERVER`. A Rex server doesn't give its blocks a
`REX_SERVER`, so the server must be started with it exported (step 2 does);
without it every stand-in command except `toast` exits with status 2 rather
than talk to the default server, which is your everyday one.

## 2. Start an isolated server

Pick a scratch directory `T` and a free port. The server gets its own XDG dirs,
so rexkit's settings and state never touch yours:

```sh
T=/path/to/scratch; PORT=51877; REXKIT=$PWD
mkdir -p "$T/state" "$T/xdg/config/rexkit" "$T/xdg/state" "$T/xdg/cache"
echo "return dofile(\"$REXKIT/lua/init.lua\")" > "$T/init.lua"
echo "return { binary = \"$T/standin/rexkit\" }" > "$T/xdg/config/rexkit/settings.lua"

env -u REX_SESSION -u REX_BLOCK -u REX_SERVER \
  REX_SERVER=http://127.0.0.1:$PORT STANDIN_LOG="$T/standin.log" \
  XDG_CONFIG_HOME="$T/xdg/config" XDG_STATE_HOME="$T/xdg/state" XDG_CACHE_HOME="$T/xdg/cache" \
  rex server run --listen http://127.0.0.1:$PORT --allow-anonymous \
    --config "$T/init.lua" --state-dir "$T/state" > "$T/server.log" 2>&1 &
```

`rex config reload` doesn't work over HTTP, because `server.config_reload`
needs the local Unix socket. After you change any Lua, restart the server.

## 3. Run

```sh
rx() { env -u REX_SESSION -u REX_BLOCK -u REX_SERVER \
  XDG_CONFIG_HOME="$T/xdg/config" XDG_STATE_HOME="$T/xdg/state" \
  rex --autostart=false -S http://127.0.0.1:$PORT "$@"; }

rx config check "$T/init.lua"         # loads cleanly
rx keymap --json > "$T/keymap.json"    # the resolved keymap
rx do lua/tests/check.lua --json --args \
  "{\"isolated\":true,\"log\":\"$T/standin.log\",\"rex\":\"$(command -v rex)\",\"server\":\"http://127.0.0.1:$PORT\"}"
```

The result is `{"ok":true,"passed":[…52 names…],"failed":{}}`. Each failure
maps the check's name to its error. `rex` and `server` are optional: without
them the double-press check (two `rex api call client.action` processes at
once) is skipped.

To see config warnings, invalid settings among them, run:

```sh
rx do rexkit_diagnostics --json
```

The warnings go to `rex.log` too, but the output of `rex.log` from a config
isn't visible anywhere.

## 4. Stop it

```sh
pkill -f "server run --listen http://127.0.0.1:$PORT"
pgrep -fl standin/rexkit   # should print nothing
```
