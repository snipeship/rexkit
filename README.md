# rexkit

rexkit is a workspace toolkit for the Rex terminal. It has
two parts:

- **A Lua config layer** (`lua/`). It adds Zellij-style modes, a tmux prefix,
  a Scratch tab for parking panes, and the actions that open the tools.
- **One Go program, `rexkit`** (`cmd/rexkit`). It contains the floating tools
  (Go to, Files, Hints, Keys, Rename, Scrollback, Search), the helpers that the Lua
  layer runs, the agent watcher that toasts when an agent needs you, and the
  installer.

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
- optional: `nvim` 0.10 or newer (Scrollback, Search, editing), `git` (Files,
  Hints), `zoxide` (Files), `docker` and `kubectl` (Hints)

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
| ⌥A | Next agent that needs you |
| ⌥⇧A | Back to where your ⌥A presses began |

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
| S | Search every pane |
| f | Hints |
| q | Leave |

### Session (⌃O)

| Key | Action |
|---|---|
| w | Go to |
| / | Search every pane |
| a | Next agent that needs you |
| A | Back to where your `a` presses began |
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
| f | Search every pane |
| F | Hints |
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
| ⌥A | Select the next agent that needs you |
| ⌃⌫ | Mark the selected agent seen (with the search empty) |
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

### Agents (`⌥A`, `⌥⇧A`, `⌃O a`)

rexkit reads the status that coding agents report (Rex's program status,
OSC 7501). An agent **needs you** when it is blocked (it asks a question,
wants a permission or a sign-in), when it failed, or when it finished and
you have not looked at it yet.

Go to lists these agents first, under Needs you: blocked, then failed, then
done, oldest first. Each row has a second line with what the agent said,
and the preview shows its question, error or result. An agent that reports
nothing is guessed from its title and process, shown dimmed with a `?`, and
never needs you. A report of work that has gone quiet for two minutes while
the pane shows an idle title or a shell is shown the same way.

A finished agent counts as seen when you go to it (from Go to, ⌥A or a
toast), when the focus moves to its pane for a second, when you mark it with
⌃⌫ in Go to, or when it starts working again. Opening Go to or its preview is
not a look. A result nobody looked at leaves Needs you after a day. A
blocked or failed agent stays until it goes on.

⌥A goes to the next agent that needs you, in any session: unseen ones
first, then round the list. A toast where you land says how many more are
waiting. ⌥⇧A goes back to where the presses began, and pressed again it
returns. In Go to, ⌥A moves the selection instead, and ⌥⇧A asks you to
close Go to first. Without the ⌥ shortcuts, `⌃O a` and `⌃O ⇧A` do the
same.

When an agent starts to need you, a toast says so in the top-right corner of
the window. Rex does not tell rexkit which session the app shows, so the
toast shows in every session, except one where that agent's pane is the
focused pane. Agents that change within a second and a half share one
toast. One agent toasts at most once in 30 seconds; news it has sooner
shows once they are up, unless you saw it by then. A click on the toast
goes to the agent. In another session that needs Remote Control; without
it, a click only closes the toast. `agent_toasts = false` turns toasts off;
`agent_toast_states` picks which states toast.

The toasts come from a watcher, `rexkit attend`, which the Lua layer starts
once for each config load. It ends on its own when the config is reloaded,
when Rex stops, or when rexkit is removed. `rexkit doctor` reports whether
the watcher runs.

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

### Hints (`⌃S f`, `⌃B ⇧F`)

Labels what is worth picking on the focused pane: URLs, file paths (with
`:line` or `:line:col`), git SHAs, UUIDs, IP addresses, and container and pod
ids. The pane is drawn again in place, dimmed, with each of them lifted and a
label of one or two letters in front of it. The labels nearest the cursor are
the easiest to type, and the same text twice gets one label.

| Key | Action |
|---|---|
| label | Copy it, or after ⇥ paste it; a toast says which |
| ⇥ | Switch between copy and paste, also after a first letter typed without ⇧ |
| ⇧ on either letter | Open it (see below), whichever ⇥ chose |
| ⌫ | Take back a letter |
| ⎋ ⌃C | Close |

Paste types the text into the pane, without ⏎, to hand a path or a SHA to
the program there (a shell, an agent, an editor). A program that asks for
bracketed paste (zsh, Neovim, most agents) gets it as a paste, so it reads
the text as text and not as keys. The key bar always says what a label
does (`a copy`, or `a paste` in the accent), also while you type a label
(`y… 9 left to paste`) and after a key that starts none, and what ⇥
switches to, except in the narrowest panes. Once a letter is typed with ⇧
the label opens and ⇥ does nothing; ⌫ takes the letter back, and with it
the open.

⇧ opens:

- a URL in the browser;
- a file in the `editor` setting, at its line (and column, for editors that
  take one), in a split beside the pane. A relative path is looked up from
  the pane's folder, then from the top of its git repository, then by the
  end of its path below the pane's folder;
- a commit in `git show`, in a split;
- a container in `docker logs -f`, in a split;
- a pod in `kubectl describe pod`, in a split, with the namespace
  (`-n` / `--namespace`) of the nearest `kubectl` command above it, or the
  pod's own row's first column after `kubectl get pods -A`.

Anything else, such as a UUID or a file that is not there, is copied instead,
and the toast says why. A key that starts no label shows `no label …` in the
key bar. A pane with nothing to pick gets no overlay, only a toast. The
`hints_key` setting adds a global key; one that another global key already
used opens Hints instead, and `rexkit doctor` says so.

Limits:

- Hints labels the pane's live screen. Rex does not tell rexkit how far a
  pane is scrolled back, so in a scrolled pane the labels are for the screen
  at the bottom of its history.
- A URL or path that wraps onto the next row is cut at the row's end, and
  the part on the next row can be labelled on its own (the tail of a
  wrapped UUID can read as a SHA).
- A SHA made only of digits, or only of the letters a to f, is not
  labelled: it reads as a number or a word.
- When Rex draws panes in a colour of its window rather than the theme's
  (its blended interface style), Hints draws the pane on the theme's
  background, which can be darker or lighter than the pane.
- ⌥ + a letter is not a key of Hints. A global ⌥ shortcut still runs while
  Hints is open, but on Hints rather than the pane under it (⌥A says to
  close the open tool first, ⌥H finds no pane beside it), so close Hints
  first; any other ⌥ letter shows `no label …`.
- Copying runs `pbcopy` and opening runs `open` on the machine that runs the
  Rex server. With a remote server, that machine's clipboard and browser get
  them.

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
shown. Opened at an older line (from Search), it keeps the lines around that
line instead, and the last line says how many newer lines are not shown.

| Key | Action |
|---|---|
| ? or /, then n / N | Search (`?` searches up from the newest output) |
| v, then y | Select, then copy |
| Y | Copy the logical line under the cursor (joins wrapped rows) |
| q, ⎋⎋ | Close |

Opened from Search, Scrollback shows the capture that was searched, at the
match's line, with the query as its search, so n and N go on from there.

### Search (`⌃S ⇧S`, `⌃O /`, `⌃B f`)

Searches the screen and scrollback of every pane, in every session, floating
ones too. The panes of the session you are in come first, then the others in
Rex's order.
Matches are grouped by pane, with the newest five of each pane listed and
the older ones folded into one row; the preview shows the lines around the
selected match. Panes are read four at a time, and their matches come in as
they are read: the tray says `Reading 8 of 12 panes`. A pane that could not
be read is counted there too.

Search ignores case until you type a capital letter (smart case). ⌥C
matches case exactly, and ⌥R reads the query as a regular expression (Go's
syntax). ⇥ limits the search to this session.

| Key | Action |
|---|---|
| type | Search |
| ⏎ | On a match: go to its pane and open Scrollback at its line. On a pane: go there. On a fold: show the earlier matches |
| ⌥⏎, double-click | Go to the pane (double-click on a fold: show the earlier matches) |
| → | Show the earlier matches (on a fold) |
| ↑ ↓ ⌃K ⌃J, PgUp PgDn | Move |
| ⌥↑ / ⌥↓ | Previous / next pane |
| ⇥ | This session only, or every session |
| ⌥C / ⌥R | Match case / regular expression |
| ⌃Y | Copy the line |
| ⌃R | Read the panes again |
| ⌃/ | Toggle preview |
| ⌃U / ⌃D | Scroll the preview |
| F1, ? | Keys (`?` only while the query is empty and ⌥R is off; otherwise it is typed) |
| ⎋ ⌃C | Close |

Limits:

- Search reads each pane once, when it opens. ⌃R reads them again.
- Each search is over the whole of each pane's history, as Scrollback shows
  it: lines that wrapped on screen are one line. A history longer than
  Scrollback keeps opens with the lines around the match, so n and N find
  only the matches near it.
- A regular expression that Neovim cannot search for in the same way (`\B`,
  or a case flag inside it) still finds its matches; Scrollback then opens
  at the line and searches for the text that matched there. In `less`
  (without Neovim), Scrollback opens at the line, without the search.
- ⌥C, ⌥R and ⌥⏎ need ⌥ to send Esc (option as meta). The chips can be
  clicked instead.
- ⌥⏎ to a pane in another session needs Remote Control, as in Go to; ⏎
  does not.
- ⏎ opens Scrollback only when Lua delivered it (the picker mode). After a
  config reload, reopen Search.

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
| `agent_toasts` | `true` | Toast when an agent in another pane needs you |
| `agent_toast_states` | `{ "blocked", "failed", "done" }` | Which agents toast: blocked (a question, a permission), failed, done; `{}` for none |
| `hints_key` | `""` | A global key for Hints, such as `"ctrl+shift+f"`; `""` for none (`⌃S f` and `⌃B ⇧F` open it, when those modes are on) |
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
- the agent watcher: does it run?
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
- **Hints copies or opens nothing.** It runs `pbcopy` and `open` on the
  machine that runs the Rex server, and finds `git`, `docker` and `kubectl`
  on the server's `$PATH` or in Homebrew's folders. The toast says what
  failed.
- **Search's ⌥C or ⌥R types a character.** ⌥ must send Esc (option as
  meta) for the ⌥ keys to reach a tool; click the chip instead.
- **`hints_key` does nothing.** A key Rex can't read is left out of the
  keymap; `rexkit doctor` lists it with the keymap diagnostics.
- **No toasts when an agent needs you.** `rexkit doctor` says whether the
  watcher runs; `rex config reload` starts it again. Its log is
  `~/.local/state/rexkit/attend/attend.log`. Check `agent_toasts` and
  `agent_toast_states`. The agent must report its status (OSC 7501).

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
- Hints: no mode while it is open, labels nearest the cursor (a hidden
  cursor counts as none), copy, ⇥ then paste (never to the clipboard, and
  as a bracketed paste to a program that asks for one), opening a URL, a
  file at its line, a commit, a container's logs and a pod, ⏎ with
  `commit_via = "global"`, a pane with nothing to pick, a zoomed pane, and
  `hints_key`;
- rename;
- Search: ⏎ on a match in another session opens Scrollback over its pane at
  the match's line with the query, F1 lists its keys, and ⌥⏎ goes to a pane;
- the Scratch round trips;
- keys reaching panes in normal and locked modes;
- the key guide;
- the agent watcher (one per config load, where toasts show, toasts off,
  seen when focused, its end when rexkit leaves the config), ⌥A and ⌥⇧A
  across sessions and without the ⌥ shortcuts, ⌥A and ⌃⌫ in Go to, and a
  click on a toast;
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
  - `tools.lua` opens floats and applies tool results. Hints opens with no
    mode, not even the picker mode: every key it reads (⇥, ⎋, ⌃C, its
    letters) is one that no global binding takes, so it reaches the float as
    it reaches any pane, and nothing is left on once Hints closes by itself.
    It still answers the handshake, for a global ⏎ (`commit_via =
    "global"`) and for ⌃⇧Space, which closes it and opens Go to.
    `rexkit_notify` shows a tool's toast. F1 is global, so `rexkit_keys`
    writes it into Search, which lists its own keys on it, instead of
    opening the guide over it.
    `rexkit_search_jump` opens Scrollback for Search's ⏎ (see below).
  - `attend.lua` starts the agent watcher and runs ⌥A and ⌥⇧A.
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
  action the tool asked for. Hints and Scrollback read ⏎ / ⎋ / ⌃C
  themselves and close on their own; Hints says what it did in a toast
  (`rexkit_notify`).
- **Search to Scrollback.** Search writes the capture it searched to
  `page.txt` in a new folder under `~/.local/state/rexkit/tools/`, and its
  ⏎ result asks Lua to run `rexkit_search_jump` with the pane, the page, the
  line and the query. Search writes the result before the page, as Lua
  waits for the result only 1.5 s. Lua waits for Search's float to go,
  then opens `rexkit scrollback --page FILE --line N --search TEXT [--regex]
  [--match-case]` over the pane. Scrollback takes the page (and removes it,
  as it is a `page.txt` in a folder of its own under `tools/`),
  so the line it opens at is the line Search listed; the query becomes a
  Neovim pattern (`internal/find`) for `n` and `N`.
- **Agent watcher.** Each config load makes a token and starts `rexkit
  attend --token T`, detached. The watcher asks the server for the token
  (`rexkit_attend_token`) and ends when it differs: an older load, or `rex
  config check`. It holds a lock per server run under
  `~/.local/state/rexkit/attend/`, so only one runs, and follows Rex's events.
  When an agent starts to need you it runs `rexkit_attend_toast` in each
  session, which opens `rexkit toast` in that session's active window.
  What was seen, and where ⌥A started, is in
  `~/.local/state/rexkit/attention.json`, which Go to, ⌥A and the watcher
  share. ⌥A and ⌥⇧A run `rexkit attend next` and
  `rexkit attend back`, and Lua carries out the jump they print.
- **Go packages**
  - `internal/rex`: typed Rex client.
  - `internal/popup`: float lifecycle, closing and results.
  - `internal/ui`: the shared look, drawn from Rex's own theme tokens.
  - `internal/rextheme`, `internal/backdrop`: Rex's theme, and the dimmed
    window around a tool's card.
  - `internal/match`: fuzzy ranking.
  - `internal/find`: Search's matching (literal, smart case or regex) and
    its translation into a Neovim pattern.
  - `internal/state`: per-tool state directories, history and attention.
  - `internal/attend`: agent status, what needs you, the watcher and ⌥A.
  - `internal/app/*`: the tools.
  - `internal/install`: install, uninstall and doctor.
  - `nvim/`: the scrollback viewer profile.

## License

MIT, see [LICENSE](LICENSE).
