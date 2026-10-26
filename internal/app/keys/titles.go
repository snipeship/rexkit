package keys

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
	"rexkit/internal/ui/sanitize"
)

// nativeTitles are the app's own actions (`rex -C <app> actions`), which the
// keymap does not describe, in the guide's sentence case.
var nativeTitles = map[string]string{
	"client.window.new":               "New window",
	"client.window.close":             "Close window",
	"client.window.fullscreen.toggle": "Toggle full screen",
	"client.close":                    "Close",
	"client.palette.toggle":           "Command palette…",
	"client.settings":                 "Settings…",
	"client.diagnostics":              "Rex diagnostics",
	"client.version.copy":             "Copy version info",
	"client.debug.copy":               "Copy debug info",
	"client.shortcuts.edit":           "Change shortcut…",
	"client.server.restart":           "Restart server…",
	"session.new":                     "New session",
	"session.switch":                  "Change session…",
	"session.select":                  "Go to session",
	"session.next":                    "Next session",
	"session.previous":                "Previous session",
	"session.rename":                  "Rename session",
	"session.close":                   "Close session",
	"session.refresh":                 "Refresh sessions",
	"session.open_on_web":             "Open session on web…",
	"client.host.switch":              "Switch host",
	"client.tab.new":                  "New tab",
	"client.tab.close":                "Close tab",
	"client.tab.rename":               "Rename tab",
	"client.tab.next":                 "Next tab",
	"client.tab.previous":             "Previous tab",
	"client.tab.goto":                 "Go to tab",
	"client.tab.move":                 "Move tab",
	"client.tab.move.backward":        "Move tab backward",
	"client.tab.move.forward":         "Move tab forward",
	"client.tab.move_to_new_window":   "Move tab to new window",
	"pane.split":                      "Split pane",
	"pane.split.left":                 "Split pane left",
	"pane.split.right":                "Split pane right",
	"pane.split.up":                   "Split pane up",
	"pane.split.down":                 "Split pane down",
	"pane.split.auto":                 "Split pane best fit",
	"pane.focus":                      "Focus pane",
	"pane.focus.left":                 "Focus pane left",
	"pane.focus.right":                "Focus pane right",
	"pane.focus.up":                   "Focus pane up",
	"pane.focus.down":                 "Focus pane down",
	"pane.focus_next":                 "Focus next pane",
	"pane.focus_previous":             "Focus previous pane",
	"pane.move":                       "Move pane",
	"pane.move.left":                  "Move pane left",
	"pane.move.right":                 "Move pane right",
	"pane.move.up":                    "Move pane up",
	"pane.move.down":                  "Move pane down",
	"pane.zoom":                       "Zoom pane",
	"pane.close":                      "Close pane",
	"pane.balance":                    "Balance splits",
	"pane.resize":                     "Resize pane",
	"pane.move_to_new_tab":            "Move pane to new tab",
	"pane.move_to_tab":                "Move pane to tab…",
	"pane.go_to_directory":            "Go to directory…",
	"pane.insert_path":                "Insert path…",
	"pane.copy_id":                    "Copy pane ID",
	"pane.send_key":                   "Send key",
	"client.key.forward":              "Send key on",
	"client.prompt.previous":          "Previous prompt",
	"client.prompt.next":              "Next prompt",
	"client.mode.enter":               "Enter mode",
	"client.mode.exit":                "Leave mode",
	"client.mode.exit_all":            "Leave mode",
	"client.find.open":                "Find…",
	"client.find.next":                "Find next",
	"client.find.previous":            "Find previous",
	"client.font.increase":            "Bigger font",
	"client.font.decrease":            "Smaller font",
	"client.font.reset":               "Reset font size",
	"client.sidebar.toggle":           "Toggle sidebar",
	"client.sidebar.activity.toggle":  "Toggle activity",
	"client.tab_style.toggle":         "Toggle tab style",
	"client.theme.change":             "Change theme…",
	"client.appearance.switch_mode":   "Switch appearance",
	"client.config.copy_path":         "Copy config path",
	"client.config.reload":            "Reload config",
}

// actionPrefixes are namespaces stripped when an action has no title.
var actionPrefixes = []string{"rexkit_", "rex_", "rz_", "rx_"}

// titleOf names what a binding does, for people: the action's own title
// (or the app's, for native actions; or the action name made readable),
// followed by its arguments.
func titleOf(b rex.Binding, actions map[string]rex.Action) string {
	switch b.Action {
	case "client.mode.enter":
		var args struct {
			Name string
			Once bool
		}
		if json.Unmarshal(b.Args, &args) == nil && args.Name != "" {
			return modeTitle(args.Name, args.Once)
		}
	case "pane.send_key":
		var args struct{ Key string }
		if json.Unmarshal(b.Args, &args) == nil && args.Key != "" {
			return "Send " + ui.KeyLabel(sanitize.Label(args.Key))
		}
	}
	a, known := actions[b.Action]
	title := ""
	var skip []string
	switch {
	case known && strings.TrimSpace(a.Title) != "":
		title = sanitize.Label(strings.TrimSpace(a.Title))
	case known:
		title = humanize(b.Action)
	default:
		var native bool
		if title, native = nativeTitles[b.Action]; !native {
			title = humanize(b.Action)
		} else if dir := direction(b.Args); dir != "" {
			if t, ok := nativeTitles[b.Action+"."+dir]; ok {
				title, skip = t, []string{"direction"} // pane.split{direction=auto} → "Split pane best fit"
			}
		}
	}
	if plumbing(a) {
		return title // its arguments only route a key
	}
	// One action bound with different arguments (grow{direction}, tab
	// goto{index}) gets one row per variant; the arguments tell them apart.
	if extra := argWords(b.Args, skip...); extra != "" {
		title += " " + extra
	}
	return title
}

// titleEscapes are the escapes (sanitize.LabelSpans) of a title that
// starts with its action's own title from Lua, so the stand-ins for
// characters the title could not show read apart from its text.
func titleEscapes(title string, a rex.Action) [][2]int {
	label, escapes := sanitize.LabelSpans(strings.TrimSpace(a.Title))
	if len(escapes) == 0 || !strings.HasPrefix(title, label) {
		return nil
	}
	return escapes
}

// modeTitle names the way into a mode: "Pane mode", "Tmux mode (one key)".
func modeTitle(name string, once bool) string {
	title := capitalize(strings.ReplaceAll(sanitize.Label(name), "_", " ")) + " mode"
	if once {
		title += " (one key)"
	}
	return title
}

// direction is a binding's direction argument, or "".
func direction(raw json.RawMessage) string {
	var args struct{ Direction string }
	if len(compactJSON(raw)) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return strings.ToLower(args.Direction)
}

// descriptionOf is an action's longer explanation, when it has one that says
// more than its title.
func descriptionOf(action string, actions map[string]rex.Action) string {
	a := actions[action]
	d := sanitize.Label(strings.TrimSpace(a.Description))
	if strings.EqualFold(d, strings.TrimSpace(a.Title)) {
		return ""
	}
	return d
}

// humanize turns an action name into words: "rz_split_auto" → "Split auto".
func humanize(name string) string {
	for _, p := range actionPrefixes {
		if rest, ok := strings.CutPrefix(name, p); ok && rest != "" {
			name = rest
			break
		}
	}
	return capitalize(strings.Join(words(sanitize.Label(name)), " "))
}

// argWords renders a binding's arguments as words: {direction="left",
// amount=15} → "left 15", {start="half-up"} → "half up". Directions come
// first; other strings and numbers follow by name; true flags read as their
// name. Arguments named in skip are left out.
func argWords(raw json.RawMessage, skip ...string) string {
	var args map[string]any
	if len(compactJSON(raw)) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	names := make([]string, 0, len(args))
	for k := range args {
		if !slices.Contains(skip, k) {
			names = append(names, k)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == b:
			return 0
		case a == "direction":
			return -1
		case b == "direction":
			return 1
		}
		return strings.Compare(a, b)
	})
	var out []string
	for _, k := range names {
		switch v := args[k].(type) {
		case string:
			out = append(out, strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(sanitize.Label(v))), " "))
		case float64:
			out = append(out, fmt.Sprint(v))
		case bool:
			if v {
				out = append(out, sanitize.Label(k))
			}
		}
	}
	return strings.Join(out, " ")
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
