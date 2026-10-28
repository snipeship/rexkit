package keys

import (
	"encoding/json"
	"fmt"
	"strings"

	"rexkit/internal/rex"
)

// demoFirst are the key settings' keys of the demo keymap, listed first in
// their rows as the live guide lists them.
var demoFirst = []string{"ctrl+shift+a", "ctrl+alt+shift+a"}

// demoKeymap is a keymap shaped like rexkit's own Lua layer, with one
// deliberate conflict (ctrl+o), for `rexkit demo keys` and tests.
func demoKeymap() rex.Keymap {
	var km rex.Keymap
	line := 0
	// bind adds a binding; the server lists the newest first.
	bind := func(key, action string, args ...any) {
		line++
		mode, key, _ := strings.Cut(key, "/")
		if key == "" {
			mode, key = "", mode
		}
		b := rex.Binding{Keys: strings.Split(key, ">"), Action: action, Mode: mode, Source: fmt.Sprintf("init.lua:%d", line)}
		if len(args) > 0 {
			obj := map[string]any{}
			for i := 0; i+1 < len(args); i += 2 {
				obj[args[i].(string)] = args[i+1]
			}
			b.Args, _ = json.Marshal(obj)
		}
		km.Bindings = append(rex.List[rex.Binding]{b}, km.Bindings...)
	}
	action := func(name, title, description string) {
		km.Actions = append(km.Actions, rex.Action{Name: name, Kind: "lua", Title: title, Description: description, Category: "rexkit"})
	}
	exits := func(mode string, extra ...string) {
		for _, k := range append([]string{"esc", "enter", "ctrl+c"}, extra...) {
			bind(mode+"/"+k, "client.mode.exit_all")
		}
	}
	dirs := [][2]string{{"h", "left"}, {"j", "down"}, {"k", "up"}, {"l", "right"}}

	action("rexkit_goto", "Go to session, tab, pane or agent", "Switch anywhere, with live agent status")
	action("rexkit_files", "Files", "Open a file or folder in a new tab, split or editor")
	action("rexkit_keys", "Key guide", "What every key does, from the live keymap")
	action("rexkit_rename_pane", "Rename pane", "")
	action("rexkit_rename_tab", "Rename tab", "")
	action("rexkit_rename_session", "Rename session", "")
	action("rexkit_floats", "Show / hide floats", "Show or hide the tab's floating panes, or open one")
	action("rexkit_float", "Float / embed", "Float the pane, or put a floating pane back in the layout")
	action("rexkit_new_pane", "New pane / split", "Split the pane, or open a floating pane from a floating pane")
	action("rexkit_scrollback", "Scrollback", "Search and copy the pane's history in Neovim")
	action("rexkit_hints", "Hints", "Copy, open or paste a URL, path or SHA from the pane")
	action("rexkit_search", "Search every pane", "Search the screen and scrollback of every pane")
	action("rexkit_settings", "Edit settings", "Open ~/.config/rexkit/settings.lua")
	action("rexkit_next_agent", "Next agent that needs you", "Go to the next agent that is blocked, failed, or done and not seen")
	action("rexkit_agent_back", "Back to where you were", "Go back to where you were before the next-agent key")
	action("rexkit_last_tab", "Last tab", "Toggle between the two most recent tabs")
	for _, d := range dirs {
		action("rexkit_navigate_"+d[1], "Navigate "+d[1], "Focus the next pane, or the next tab at the edge")
	}

	bind("ctrl+o", "pane.go_to_directory")
	for _, m := range [][2]string{{"ctrl+p", "pane"}, {"ctrl+t", "tab"}, {"ctrl+n", "resize"}, {"ctrl+h", "move"}, {"ctrl+s", "scroll"}, {"ctrl+o", "session"}} {
		bind(m[0], "client.mode.enter", "name", m[1])
	}
	bind("ctrl+b", "client.mode.enter", "name", "tmux", "once", true)
	bind("ctrl+g", "client.mode.enter", "name", "locked")
	bind("ctrl+shift+space", "rexkit_goto")
	bind("f1", "rexkit_keys")
	for _, d := range dirs {
		bind("alt+"+d[0], "rexkit_navigate_"+d[1])
	}
	bind("alt+n", "rexkit_new_pane")
	bind("alt+a", "rexkit_next_agent")
	bind("alt+shift+a", "rexkit_agent_back")
	bind("ctrl+shift+a", "rexkit_next_agent")     // agent_key
	bind("ctrl+alt+shift+a", "rexkit_agent_back") // agent_back_key
	bind("alt+i", "client.tab.move", "direction", "left")
	bind("alt+o", "client.tab.move", "direction", "right")

	for _, d := range dirs {
		bind("pane/"+d[0], "pane.focus", "direction", d[1])
		bind("pane/"+d[1], "pane.focus", "direction", d[1])
	}
	bind("pane/p", "pane.focus_next")
	bind("pane/shift+p", "pane.focus_previous")
	bind("pane/r", "pane.split", "direction", "right")
	bind("pane/d", "pane.split", "direction", "down")
	bind("pane/n", "rexkit_new_pane")
	bind("pane/f", "pane.zoom")
	bind("pane/x", "pane.close")
	bind("pane/=", "pane.balance")
	bind("pane/c", "rexkit_rename_pane")
	bind("pane/o", "rexkit_files")
	bind("pane/e", "rexkit_float")
	bind("pane/w", "rexkit_floats")
	bind("pane/i", "pane.insert_path")
	bind("pane/g", "pane.go_to_directory")
	exits("pane", "ctrl+p")

	bind("tab/h", "client.tab.previous")
	bind("tab/l", "client.tab.next")
	bind("tab/tab", "rexkit_last_tab")
	for i := 1; i <= 3; i++ {
		bind(fmt.Sprintf("tab/%d", i), "client.tab.goto", "index", i)
	}
	bind("tab/n", "client.tab.new")
	bind("tab/r", "rexkit_rename_tab")
	bind("tab/x", "client.tab.close")
	bind("tab/b", "pane.move_to_new_tab")
	exits("tab", "ctrl+t")

	for _, d := range dirs {
		bind("resize/"+d[0], "pane.resize", "direction", d[1])
	}
	bind("resize/0", "pane.balance")
	exits("resize", "ctrl+n")

	for _, d := range dirs {
		bind("move/"+d[0], "pane.move", "direction", d[1])
	}
	exits("move", "ctrl+h")

	bind("scroll/e", "rexkit_scrollback")
	bind("scroll/k", "client.prompt.previous")
	bind("scroll/j", "client.prompt.next")
	bind("scroll//", "client.find.open")
	bind("scroll/f", "rexkit_hints")
	bind("scroll/shift+s", "rexkit_search")
	exits("scroll", "ctrl+s", "q")

	bind("session/w", "rexkit_goto")
	bind("session//", "rexkit_search")
	bind("session/a", "rexkit_next_agent")
	bind("session/shift+a", "rexkit_agent_back")
	bind("session/n", "session.new")
	bind("session/r", "rexkit_rename_session")
	bind("session/j", "session.next")
	bind("session/k", "session.previous")
	bind("session/p", "client.palette.toggle")
	bind("session/c", "rexkit_settings")
	bind("session/?", "rexkit_keys")
	exits("session", "ctrl+o")

	bind("tmux/c", "client.tab.new")
	bind("tmux/n", "client.tab.next")
	bind("tmux/p", "client.tab.previous")
	bind("tmux/z", "pane.zoom")
	bind("tmux/shift+f", "rexkit_hints")
	bind("tmux/f", "rexkit_search")
	bind("tmux/shift+5", "pane.split", "direction", "right")
	bind("tmux/shift+'", "pane.split", "direction", "down")

	bind("locked/ctrl+g", "client.mode.exit_all")

	km.Modes = rex.List[rex.Mode]{{Name: "locked", Exclusive: true}, {Name: "pane"}, {Name: "tab"}, {Name: "resize"}, {Name: "move"}, {Name: "scroll"}, {Name: "session"}, {Name: "tmux"}}
	return km
}
