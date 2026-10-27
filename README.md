# rexkit

rexkit is a workspace toolkit for the Rex terminal. It has
two parts:

- **A Lua config layer** (`lua/`). It adds Zellij-style modes, a tmux prefix,
  a Scratch tab for parking panes, and the actions that open the tools.
- **One Go program, `rexkit`** (`cmd/rexkit`). It contains the floating tools
  (Go to, Files, Keys, Rename, Scrollback), the helpers that the Lua layer
  runs, and the installer.

Every tool opens as a floating layer over the pane you were in. When you
dismiss it, focus goes back to that pane, and a zoomed pane is zoomed again.



https://github.com/user-attachments/assets/d1d325c4-4cff-4e40-b755-0541d977c1b7



https://github.com/user-attachments/assets/0e37a114-cede-4917-ba2e-ba1ea14ea684



## Install

With Homebrew:

```sh
brew install snipeship/tap/rexkit
rexkit install                         # --dry-run shows the changes and checks the new config first
```

After `brew upgrade rexkit`, run `rexkit install` again so that Rex reloads
the new version.

From source (needs Go 1.27):

```sh
git clone https://github.com/snipeship/rexkit.git ~/Coding/rexkit && cd ~/Coding/rexkit
go run ./cmd/rexkit install
```

After you pull, run `go run ./cmd/rexkit install` again, so that the binary
is rebuilt.

`install` does the following:

1. Builds `bin/rexkit` from source, or, installed with Homebrew, checks the
   binary it came with.
2. Backs up your current `~/.config/rex/init.lua`.
3. Replaces it with a small loader that runs rexkit's `lua/init.lua`: the
   one in your clone, or Homebrew's under `opt/rexkit/libexec`, which stays
   the same across upgrades.
4. Checks the result with `rex config check` (a Lua module that fails to load
   fails the check), reloads the running server, and asks the reloaded layer
   whether every module loaded. On any failure it puts the old config back.
5. Runs `doctor`. Install exits 1 when doctor finds a problem.

`rexkit uninstall` restores the backed-up config (from a clone:
`go run ./cmd/rexkit uninstall`). Pass `--to FILE` to restore a different
file. With Homebrew, run `rexkit uninstall` before `brew uninstall rexkit`:
Rex's config loads rexkit's files, so removing them first leaves Rex with a
config that fails to load. (If that happened, `brew install
snipeship/tap/rexkit && rexkit uninstall` puts your old config back.) To switch between a clone and Homebrew, run the other one's `install`;
`--root DIR` picks a clone explicitly.

Requirements:

- macOS
- a Rex build with floating layers (`session.new_layer`)
- optional: `nvim` 0.10 or newer (Scrollback, editing), `git` and `zoxide`
  (Files)

## Keys

Each prefix enters its mode from normal mode. Every mode except Locked shares
these keys:

- ⎋, ⏎, ⌃C and the mode's own prefix leave the mode. (In Tmux, ⌃B sends ⌃B
  to the pane instead.)
- Another mode's prefix switches straight to that mode.
- `?` opens the key guide for the mode. In Session mode, `?` opens the whole
  guide.

Inside a mode, the text keys the mode does not bind are blocked, so typing
never leaks into the terminal. The global shortcuts below and every ⌘ key
still work. In normal mode rexkit does not bind ⏎, ⎋ or ⌃C, so those keys
always reach the pane.

In the tables, a capital letter means ⇧ plus that letter (`R` is ⇧R), and
`⌃P e` means ⌃P, then e.

| Prefix | Mode |
|---|---|
| ⌃P | Pane |
| ⌃T | Tab |
| ⌃N | Resize |
| ⌃H | Move |
| ⌃S | Scroll |
| ⌃O | Session |
| ⌃B | Tmux prefix (one key, then back to normal) |
| ⌃G | Locked |

The modes are optional. Set `zellij_modes = false` to drop ⌃P ⌃T ⌃N ⌃H ⌃S
⌃O, or `tmux_prefix = false` to drop ⌃B; their keys then reach the terminal
again. ⌃G stays while either is on. The global keys below stay either way,
and every tool is still in Rex's command palette.

### Global

| Key | Action |
|---|---|
| ⌃⇧Space | Go to |
| F1 | Key guide |
| ⌥H / ⌥L | Focus left / right; at the edge, previous / next tab |
| ⌥J / ⌥K | Focus down / up |
| ⌥N | New pane (split in the longer direction) |
| ⌥I / ⌥O | Move tab backward / forward |
| ⌥= ⌥+ / ⌥- | Grow / shrink pane |

The ⌥ shortcuts are on by default. Set `alt_shortcuts = false` to turn them off.

### Pane (⌃P)

| Key | Action |
|---|---|
| h j k l, arrows | Focus in that direction |
| p / P | Focus next / previous pane |
| r / d | Split right / down |
| R / D | Split left / up |
| n | New pane (auto direction) |
| f | Zoom / restore |
| x | Close pane |
| = | Balance splits |
| i | Insert path |
| g | Go to folder |
| c | Rename pane |
| w | Show / hide Scratch |
| e | Park pane in Scratch / restore it to where it was |
| o | Files |

### Tab (⌃T)

| Key | Action |
|---|---|
| h k ← ↑ / l j → ↓ | Previous / next tab |
| Tab | Last tab |
| H / L | Move tab backward / forward |
| 1–9 | Go to tab |
| n | New tab |
| x | Close tab |
| r | Rename tab |
| b | Move pane to a new tab |
| [ / ] | Move pane to the previous / next tab |
| m | Choose a tab for the pane |

### Resize (⌃N)

| Key | Action |
|---|---|
| h j k l, arrows | Grow towards that side |
| ⇧ + h j k l | Shrink from that side |
| = + / - | Grow / shrink |
| 0 | Balance |

### Move (⌃H)

| Key | Action |
|---|---|
| h j k l, arrows | Move pane in that direction |
| n, Tab | Swap with next pane |
| p, ⇧Tab | Swap with previous pane |

### Scroll (⌃S)

| Key | Action |
|---|---|
| e, d, PgDn | Scrollback viewer, at the bottom |
| b | Scrollback viewer, at the top |
| u | Scrollback viewer, half a page up |
| PgUp | Scrollback viewer, a page up |
| k ↑ / j ↓ | Previous / next prompt |
| / s | Find |
| n / N p | Next / previous match |
| q | Leave |

### Session (⌃O)

| Key | Action |
|---|---|
| w | Go to |
| a | Next agent waiting for you |
| n | New session |
| r | Rename session |
| j / k | Next / previous session |
| c | rexkit settings (opens `settings.lua`) |
| ? | Key guide |
| s | Rex settings |
| p | Command palette |
| b | Toggle sidebar |
| d | Close this window |
| R | Reload config |

### Tmux prefix (⌃B, one key)

One key, then back to normal mode. A letter, digit, punctuation or ⌃-letter
key that Tmux does not bind just leaves the prefix, as in tmux, and so does a
global shortcut: neither reaches the pane.

| Key | Action |
|---|---|
| h j k l, arrows | Focus |
| c | New tab |
| n / p | Next / previous tab |
| z | Zoom |
| x | Close pane |
| o | Next pane |
| , | Rename tab |
| % / " | Split right / down |
| d | Close window |
| [ | Scroll mode |
| ? | Key guide for Tmux |
| ⌃B | Send ⌃B to the pane |

### Locked (⌃G)

Every key, including rexkit's global shortcuts and ⎋, goes to the pane. Only
⌃G leaves Locked mode.

## Tools

The Lua actions open each tool in a float, so you rarely run them by hand.
`rexkit demo <tool>` runs a tool with sample data in any terminal.

While a tool is open, ⏎, ⎋ and ⌃C go to it, even when you press them before
it has drawn. A ⏎ typed early waits until the tool's data has loaded.
⌃⇧Space in any other tool closes it and opens Go to. In the list tools,
⌃J / ⌃K move down / up like ↓ / ↑.

### Go to (`⌃⇧Space`, `⌃O w`)

A switcher for sessions, tabs, panes and agents.

| Key | Action |
|---|---|
| type | Filter |
| ⏎, double-click | Go there (works across sessions) |
| ⌥⏎ | Bring the pane here (same session) |
| → / ← ⌫ | Drill into / back out of a session or tab |
| ⇥ / ⇧⇥ | Next / previous category |
| ↑ ↓ ⌃K ⌃J, PgUp PgDn | Move |
| ⌃E | Rename the selected session, tab or pane |
| ⌃X | Close the selected session, tab or pane (asks first) |
| ⌃A | New session |
| ⌃R | Refresh |
| ⌃/ | Toggle preview |
| ⌃U / ⌃D | Scroll the preview |
| ? | Help |
| ⎋ ⌃C | Close |

⌃E, ⌃X and ⌃A open a small sheet over the list: ⏎ does it, ⎋ backs out.
Renaming, and closing a place you are not in, leave Go to open on the
updated list. Closing a pane that is its tab's only pane closes the tab, and
a tab that is its session's only tab closes the session; the sheet says so,
and warns about agents at work. The last session can't be closed. Closing
the place Go to was opened from, and making a new session, close Go to
first.

### Files (`⌃P o`)

A file and folder browser. Its start page lists recent places and zoxide
projects. It opens with no row selected, so ⏎, ⌥⏎ and ⇧⏎ act on the folder
you are in. ↓ selects the first row and ↑ the last; moving past either end of
the list goes back to the folder.

| Key | Action |
|---|---|
| type | Filter |
| ↑ ↓ ⌃K ⌃J, PgUp PgDn | Move |
| ⏎ | Open in a new tab (a file opens in the editor) |
| ⌥⏎ / ⇧⏎ | Open in a split to the right / below |
| ⌃E | Open in the editor (`editor` setting) |
| ⌃Y | Copy the path |
| ⌃X | Open with the default app |
| → / ← ⌫ | Enter folder / parent folder |
| ⌃L | Type a path (or click the current folder name twice) |
| ⌃F | Toggle recursive search |
| ⌥. | Toggle hidden files |
| ⌃R | Refresh |
| ⌃/ | Toggle preview (⌃U / ⌃D scroll it) |
| ? | Help |
| ⎋ ⌃C | Close (⎋ first leaves the path field) |

### Keys (`F1`, `?` in any mode)

A searchable guide built from the live keymap. ⏎ runs the selected action in
the pane you came from. ⇥ / ⇧⇥ step through the modes, and ⎋ clears the
filter or closes the guide.

### Rename (`⌃P c`, `⌃T r`, `⌃O r`, `⌃B ,`)

Renames a pane, tab or session. ⏎ saves, ⎋ or ⌃C cancels, and ⌃U clears the
name. Saving an empty name gives a pane or tab its automatic name back; for a
session, Rex picks a new one.

### Scrollback (`⌃S e/d/b/u/PgUp/PgDn`)

Shows the pane's scrollback, with colour, in Neovim (0.10 or newer), or in
`less` when Neovim is missing. `e`, `d` and PgDn start at the bottom, `b` at
the top, `u` half a page up and PgUp a page up. Very long histories keep the
newest lines that fit, and the first line says how many older lines are not
shown.

| Key | Action |
|---|---|
| ? or /, then n / N | Search (`?` searches up from the newest output) |
| v, then y | Select, then copy |
| Y | Copy the logical line under the cursor (joins wrapped rows) |
| q, ⎋⎋ | Close |

### Scratch

Scratch is a tab that holds parked panes. `⌃P e` parks the focused pane there
and leaves a placeholder in its place. Press `⌃P e` again, from either side,
to bring the pane back. Parking moves the same pane, so the process keeps
running. When the pane was the only one in its tab, the placeholder keeps
that tab open. The placeholder ignores ⌃C, ⌃Z and other keys. If the original
tab was closed meanwhile, the pane comes back in a new tab.

## Settings

Put overrides in `~/.config/rexkit/settings.lua` (or under
`$XDG_CONFIG_HOME/rexkit/settings.lua`). The file returns a table:

```lua
return { shell = "/bin/zsh", resize_step = 0.1 }
```

`⌃O c` opens the file, and creates it with every default listed when it does
not exist. Run `⌃O ⇧R` to reload after you edit it. `rexkit doctor` warns
about unknown names, bad values, and a `shell` or `editor` that does not
exist (a missing shell falls back to `$SHELL`). A leading `~/` in `shell`,
`editor` and `binary` means your home folder.

| Setting | Default | Meaning |
|---|---|---|
| `shell` | `/opt/homebrew/bin/nu` | Shell for new panes, the Scratch tab and Files |
| `editor` | `{ "/opt/homebrew/bin/nvim" }` | Editor command; the file is appended as one more argument |
| `resize_step` | `0.05` | How far one resize key moves a border (fraction of the split) |
| `scratch_label` | `"Scratch"` | Name of the tab that holds parked panes |
| `zellij_modes` | `true` | The Zellij-style modes: ⌃P pane, ⌃T tab, ⌃N resize, ⌃H move, ⌃S scroll, ⌃O session |
| `tmux_prefix` | `true` | The tmux-style ⌃B prefix (one key, then back to normal) |
| `alt_shortcuts` | `true` | The global ⌥ shortcuts |
| `compact_labels` | `true` | Short action titles in mode panels and the palette |
| `commit_via` | `"mode"` | How ⏎ reaches an open tool: `"mode"` (a picker mode) or `"global"` (a global ⏎ binding) |
| `dismiss_on_blur` | `true` | Close a tool when it loses focus |
| `backdrop` | `"clear"` | Around a tool's card: `"clear"` (the live panes) or `"dim"` (a dimmed copy of them, with a shadow) |
| `binary` | `"bin/rexkit"` | The rexkit program; a relative path is resolved against rexkit's files (your clone, or Homebrew's `opt/rexkit/libexec`) |

## Troubleshooting

```sh
rexkit doctor          # from a clone: bin/rexkit doctor
```

`doctor` checks, and explains each problem it finds:

- the binary: is it present, and (in a clone) is it older than the sources?
- the loader in `~/.config/rex/init.lua`
- your settings
- `rex config check`
- whether the running server has loaded the current config
- the Lua layer: did every module load?
- keymap diagnostics and conflicting bindings
- remote control and floating-layer support
- stale tool state
- `nvim`, `git` and `zoxide`

It exits 1 when something fails.

Common fixes:

- **A tool does not open, or opens and closes at once.** Run `rexkit
  install` again (from a clone: `go run ./cmd/rexkit install`, which also
  rebuilds `bin/rexkit`). It reloads the config.
- **Keys do nothing after an edit.** Press `⌃O ⇧R`, or run `rex config
  reload`. Then look at `rex config check` for errors.
- **A tool float is stuck.** ⎋ closes it. If the tool crashed, closing the
  float pane with ⌃P x is safe.

## Development

```sh
go build ./... && go vet ./... && gofmt -l cmd internal tests nvim lua
go test -race ./...                          # unit tests; never touch a Rex server
go test -tags integration ./tests/... ./internal/app/scrollback/
```

The integration tests start their own isolated Rex server for each test. Each
server has the following:

- an HTTP listener (a Unix socket for the packaged-install test: Rex reloads
  its config only through its socket);
- private XDG directories;
- a config that is a loader for `lua/init.lua`;
- a `settings.lua` that uses `/bin/sh`, `vi` and a rexkit built for the run.

`TestMain` builds rexkit into a temporary directory first and deletes it
afterwards. It never writes the clone's `bin/rexkit`, so running the suite
in the clone you installed from leaves the live binary alone. The tests never connect to your running
server, and they clear `REX_SERVER`, `REX_SESSION` and `REX_BLOCK`
themselves. The suite covers:

- every tool float, with focus and zoom restored on dismiss;
- the Go to handshake, including keys typed before the tool is up and
  cross-session targets;
- Files verbs;
- rename;
- the Scratch round trips;
- keys reaching panes in normal and locked modes;
- the key guide;
- a Homebrew-style packaged install and upgrade (`TestPackagedInstall`);
- `lua/tests/check.lua`, run through `TestLuaChecks` against a stand-in
  binary. See `lua/tests/README.md`.

`tests/integration/harness` contains the server, key and drive helpers.

### Snapshots

`rexkit snap` draws a tool in a scripted state to a PNG, the way Rex shows it
(14×29 px cells, Berkeley Mono, the merino themes), so a design can be judged
from pictures. It runs the tool's demo data, never a Rex server:

```sh
rexkit snap -list                                   # tools and their states
rexkit snap --tool goto --state search --out goto.png
rexkit snap --tool rename --state error --window --theme light --out rename.png
rexkit snap --all --theme both --out-dir /tmp/snaps # every tool × state, plus index.txt
rexkit snap --all --window --theme both --out-dir /tmp/snaps-window # only in their floats
```

`--window` draws the tool in its card of a whole-window float (the card sizes
of `lua/tools.lua`) over `backdrop.Demo`'s made-up panes, fully dimmed, as
the `dim` setting shows them (`--backdrop clear` draws them undimmed, as
`clear` shows the real panes; `--backdrop none` leaves the margin
see-through; toasts and tools without a card never get one). Files with the
backdrop end in `-window-demo.png`. `--scale 1` draws at half size and
`--size COLSxROWS` sets the window (default 241x70). Each tool's states are in its `snap.go`; the renderer is
`internal/ui/snap`. Glyphs that came from a fallback font, or from none, are
reported on stderr.

## Architecture

- **Lua layer** (`lua/`)
  - `init.lua` loads settings and the modules.
  - `modes.lua` and `bindings.lua` define the keymap.
  - `tools.lua` opens floats and applies tool results.
  - `workspace.lua` implements Scratch.
  - `layout.lua` contains the split and tab helpers.
- **Tool handshake.** Lua opens the tool with `--session`, `--origin-block`,
  `--origin-window`, `--state DIR` and, when the origin was zoomed,
  `--zoomed`. To deliver ⏎ / ⎋ / ⌃C it sends the key to the tool's pty and
  runs `rexkit await` (1.5 s). A ⏎ goes with a one-shot marker,
  `DIR/commit`, that the tool takes, so it can tell Lua's ⏎ from one typed
  while the mode was off. The tool writes `DIR/result.json`, which holds one of
  `goto`, `files.open`, `rename`, `invoke`, `close` or `stay` (also the
  answer to a ⏎ the tool still holds after 1 s). Lua then
  applies the result: it leaves the mode, focuses the target (or zooms it, if
  it was the zoomed origin), selects the target's session, and runs any
  action the tool asked for.
- **Go packages**
  - `internal/rex`: typed Rex client.
  - `internal/popup`: float lifecycle, closing and results.
  - `internal/ui`: the shared look, drawn from Rex's own theme tokens.
  - `internal/rextheme`, `internal/backdrop`: Rex's theme, and the dimmed
    window around a tool's card.
  - `internal/match`: fuzzy ranking.
  - `internal/state`: per-tool state directories.
  - `internal/app/*`: the tools.
  - `internal/install`: install, uninstall and doctor.
  - `nvim/`: the scrollback viewer profile.

## License

MIT, see [LICENSE](LICENSE).
