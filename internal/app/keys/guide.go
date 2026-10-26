package keys

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"rexkit/internal/rex"
	"rexkit/internal/ui"
)

// internalModes are modes the guide never shows: rexkit's own picker mode
// only routes ⏎/⎋ to an open tool.
var internalModes = []string{"picker"}

// internalCategory marks rexkit's plumbing actions (the global ⏎ that
// confirms in a tool when commit_via is "global"); the guide leaves them out.
const internalCategory = "rexkit internal"

func plumbing(a rex.Action) bool { return a.Category == internalCategory }

// Seq is a key sequence as the server writes it: {"ctrl+b", "c"}.
type Seq []string

// Label is the sequence in glyph notation: "⌃B c".
func (s Seq) Label() string { return ui.KeyLabel(s.Chord()) }

// Chord is the sequence as one config key string, its steps a space apart
// ("ctrl+b c"), as ui.KeyLabel and ui.CapLabel read it.
func (s Seq) Chord() string { return strings.Join(s, " ") }

// Config is the sequence in config notation: "ctrl+b>c".
func (s Seq) Config() string { return strings.Join(s, ">") }

func (s Seq) equal(o Seq) bool { return slices.Equal(s, o) }

// shadows reports whether s and o cannot both work: the same key, or one a
// strict prefix of the other (a key either runs an action or starts a
// sequence, never both).
func (s Seq) shadows(o Seq) bool {
	n := min(len(s), len(o))
	return n > 0 && slices.Equal(s[:n], o[:n])
}

// Guide is the keymap grouped for reading: global keys first, then one group
// per mode in the order their prefix keys were bound.
type Guide struct {
	Groups    []Group
	Conflicts []Conflict
}

// Group is the bindings of one mode ("" is the global group).
type Group struct {
	Mode      string
	Prefix    Seq  // the global key that enters the mode, nil when none
	Once      bool // the mode ends after one of its keys
	Exclusive bool
	Entries   []Entry
}

// Name is the group's display name: "Global", "Pane".
func (g Group) Name() string {
	if g.Mode == "" {
		return "Global"
	}
	return capitalize(strings.ReplaceAll(g.Mode, "_", " "))
}

// Entry is one action of a group with every key that runs it.
type Entry struct {
	Keys        []Seq
	Action      string // "" for a summary row, which has nothing to run
	Args        json.RawMessage
	Title       string
	Escapes     [][2]int // where Title shows escaped characters (sanitize.LabelSpans)
	Description string
	Exit        bool // leaves the mode
	Conflict    int  // index into Guide.Conflicts, -1 for none
}

// KeysLabel is the entry's keys in glyph notation: "H ←".
func (e Entry) KeysLabel() string {
	labels := make([]string, len(e.Keys))
	for i, k := range e.Keys {
		labels[i] = k.Label()
	}
	return strings.Join(labels, " ")
}

// Conflict is a key that is bound more than once in a mode: the newest
// binding runs, the others never do.
type Conflict struct {
	Mode  string
	Keys  Seq
	Wins  string   // title of the binding that runs
	Loses []string // the bindings that never run: "Files (⌃O)"
}

// String explains the conflict: "⌃O runs Session mode, not Files".
func (c Conflict) String() string {
	return c.Keys.Label() + " runs " + c.Wins + ", not " + strings.Join(c.Loses, ", ")
}

// binding is a server binding with its registration order (0 = oldest).
type binding struct {
	rex.Binding
	seq    Seq
	age    int
	hidden bool // plumbing or a pass-through: it can win or lose a key, but is not listed
	lost   int  // a listed binding that a hidden one shadows: its conflict; -1 otherwise
}

func (b binding) same(o binding) bool {
	return b.Action == o.Action && bytes.Equal(compactJSON(b.Args), compactJSON(o.Args))
}

// Build groups a keymap. The server lists bindings newest first; when two
// bindings collide in a mode the newest one is the one that runs.
func Build(km rex.Keymap) Guide {
	actions := map[string]rex.Action{}
	for _, a := range km.Actions {
		actions[a.Name] = a
	}
	modes := map[string]rex.Mode{}
	for _, m := range km.Modes {
		modes[m.Name] = m
	}
	title := titler(km, actions, modes)

	var g Guide
	live := map[string][]binding{} // per mode, the bindings that run, newest first
	winners := map[string]int{}    // mode + key → conflict index
	var shadowed []binding         // listed bindings that lose to a hidden one
	for i, raw := range km.Bindings {
		b := binding{Binding: raw, seq: normalize(raw.Keys), age: len(km.Bindings) - 1 - i, lost: -1}
		if len(b.seq) == 0 || slices.Contains(internalModes, b.Mode) {
			continue
		}
		// Hidden bindings still take part: a pass-through or rexkit's own ⏎
		// can be what really runs on a key the guide would otherwise list.
		b.hidden = passThrough(b) || plumbing(actions[b.Action])
		winner := -1
		for j, w := range live[b.Mode] {
			if w.seq.shadows(b.seq) {
				winner = j
				break
			}
		}
		if winner < 0 {
			live[b.Mode] = append(live[b.Mode], b)
			continue
		}
		w := live[b.Mode][winner]
		if (w.same(b) && w.seq.equal(b.seq)) || (w.hidden && b.hidden) {
			continue // the same binding twice, or nothing the guide shows
		}
		id := b.Mode + "/" + w.seq.Config()
		k, ok := winners[id]
		if !ok {
			k = len(g.Conflicts)
			winners[id] = k
			g.Conflicts = append(g.Conflicts, Conflict{Mode: b.Mode, Keys: w.seq, Wins: title(w.Binding)})
		}
		lose := title(b.Binding)
		if !b.seq.equal(w.seq) {
			lose += " (" + b.seq.Label() + ")"
		}
		g.Conflicts[k].Loses = append(g.Conflicts[k].Loses, lose)
		if w.hidden && !b.hidden {
			// The reader would see this key and nothing else on it: list
			// it, flagged, though it never runs.
			b.lost = k
			shadowed = append(shadowed, b)
		}
	}

	listed := map[string][]binding{} // per mode, what the guide lists, oldest first
	for mode, bs := range live {
		for _, b := range bs {
			if !b.hidden {
				listed[mode] = append(listed[mode], b)
			}
		}
	}
	for _, b := range shadowed {
		listed[b.Mode] = append(listed[b.Mode], b)
	}
	for mode, bs := range listed {
		slices.SortFunc(bs, func(a, b binding) int { return a.age - b.age })
		listed[mode] = bs
	}
	conflict := func(b binding) int {
		if b.lost >= 0 {
			return b.lost
		}
		if k, ok := winners[b.Mode+"/"+b.seq.Config()]; ok {
			return k
		}
		return -1
	}
	g.Groups = groups(listed, modes, actions)
	for gi := range g.Groups {
		grp := &g.Groups[gi]
		grp.Entries = entries(listed[grp.Mode], actions, title, conflict)
	}
	for gi := range g.Groups {
		if g.Groups[gi].Mode != "" {
			g.Groups[gi].Entries = foldHops(g.Groups[gi], g.Groups, actions)
		}
	}
	return g
}

// titler names bindings for the guide. A Lua wrapper that enters a mode
// (rexkit_mode_tmux) reads the same as the native way in from the global
// group ("Tmux mode (one key)"), whatever the wrapper's own title says.
func titler(km rex.Keymap, actions map[string]rex.Action, modes map[string]rex.Mode) func(rex.Binding) string {
	once := map[string]bool{}
	for _, b := range km.Bindings {
		if name, o := entersMode(b, actions); name != "" && b.Action == "client.mode.enter" {
			once[name] = once[name] || o
		}
	}
	return func(b rex.Binding) string {
		if b.Action != "client.mode.enter" {
			if name, _ := entersMode(b, actions); name != "" {
				if _, ok := modes[name]; ok {
					return modeTitle(name, once[name])
				}
			}
		}
		return titleOf(b, actions)
	}
}

// guideAction is rexkit's key guide; bound in a mode with {mode = <that
// mode>}, it opens this guide on the mode, which is what is showing.
const guideAction = "rexkit_keys"

// foldHops shortens a mode's entries. Every rexkit mode binds the other
// modes' global prefixes to switch straight to them, and ? to its guide:
// listed in each mode they would be a third of the sheet, and the global
// group already lists every prefix. They fold into one quiet "Switch mode"
// row. Entries in a conflict always stay.
func foldHops(grp Group, all []Group, actions map[string]rex.Action) []Entry {
	prefix := map[string]Seq{}
	for _, o := range all {
		if o.Mode != "" && o.Prefix != nil {
			prefix[o.Mode] = o.Prefix
		}
	}
	var out []Entry
	var hops []Seq
	for _, e := range grp.Entries {
		if e.Conflict >= 0 || len(e.Keys) != 1 {
			out = append(out, e)
			continue
		}
		b := rex.Binding{Action: e.Action, Args: e.Args, Mode: grp.Mode}
		if other, _ := entersMode(b, actions); other != "" && other != grp.Mode && prefix[other].equal(e.Keys[0]) {
			hops = append(hops, e.Keys[0])
			continue
		}
		if e.Action == guideAction && argMode(e.Args) == grp.Mode {
			continue // the guide for this mode is what is showing
		}
		out = append(out, e)
	}
	if len(hops) == 0 {
		return out
	}
	hop := Entry{Keys: hops, Title: "Switch mode", Description: "The other modes' keys switch straight to them", Exit: true, Conflict: -1}
	at := slices.IndexFunc(out, func(e Entry) bool { return e.Exit })
	if at < 0 {
		at = len(out)
	}
	return slices.Insert(out, at, hop)
}

func argMode(raw json.RawMessage) string {
	var args struct{ Mode string }
	if len(compactJSON(raw)) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return args.Mode
}

// groups lists the global group and every mode with bindings, modes ordered
// by when their global prefix key was bound.
func groups(live map[string][]binding, modes map[string]rex.Mode, actions map[string]rex.Action) []Group {
	type ordered struct {
		Group
		order int
	}
	byMode := map[string]*ordered{}
	for mode, bs := range live {
		if mode == "" {
			continue
		}
		byMode[mode] = &ordered{Group: Group{Mode: mode, Exclusive: modes[mode].Exclusive}, order: 1 << 30}
		if len(bs) > 0 {
			byMode[mode].order = 1<<20 + bs[0].age // modes without a prefix: after the rest
		}
	}
	for _, b := range live[""] {
		if b.lost >= 0 {
			continue // never runs, so it enters nothing
		}
		name, once := entersMode(b.Binding, actions)
		o, ok := byMode[name]
		if !ok || o.Prefix != nil {
			continue
		}
		o.Prefix, o.Once, o.order = b.seq, once, b.age
	}
	out := []Group{}
	if len(live[""]) > 0 {
		out = append(out, Group{})
	}
	var rest []*ordered
	for _, o := range byMode {
		rest = append(rest, o)
	}
	slices.SortFunc(rest, func(a, b *ordered) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return strings.Compare(a.Mode, b.Mode)
	})
	for _, o := range rest {
		out = append(out, o.Group)
	}
	return out
}

// entries merges a mode's bindings (oldest first) into one entry per action,
// in the order the actions were first bound; mode exits go last.
func entries(bs []binding, actions map[string]rex.Action, title func(rex.Binding) string, conflict func(binding) int) []Entry {
	var out []Entry
	index := map[string]int{}
	for _, b := range bs {
		id := b.Action + " " + string(compactJSON(b.Args))
		if i, ok := index[id]; ok {
			out[i].Keys = append(out[i].Keys, b.seq)
			if out[i].Conflict < 0 {
				out[i].Conflict = conflict(b)
			}
			continue
		}
		index[id] = len(out)
		t := title(b.Binding)
		out = append(out, Entry{
			Keys:        []Seq{b.seq},
			Action:      b.Action,
			Args:        b.Args,
			Title:       t,
			Escapes:     titleEscapes(t, actions[b.Action]),
			Description: descriptionOf(b.Action, actions),
			Exit:        b.Mode != "" && isExit(b.Binding, actions),
			Conflict:    conflict(b),
		})
	}
	slices.SortStableFunc(out, func(a, b Entry) int {
		switch {
		case a.Exit == b.Exit:
			return 0
		case a.Exit:
			return 1
		}
		return -1
	})
	return out
}

func normalize(keys []string) Seq {
	seq := make(Seq, 0, len(keys))
	for _, k := range keys {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			seq = append(seq, k)
		}
	}
	return seq
}

// passThrough reports a binding that sends its own key on to the terminal,
// which is what an unbound key does anyway (locked mode is full of them).
func passThrough(b binding) bool {
	if b.Action != "pane.send_key" || len(b.seq) != 1 {
		return false
	}
	var args struct{ Key string }
	if json.Unmarshal(b.Args, &args) != nil || args.Key == "" {
		return false
	}
	// Compare what the keys are, not how they are spelled: the server
	// stores "escape" as "esc", and keeps the spelling it was sent in args.
	return ui.KeyLabel(strings.ToLower(strings.TrimSpace(args.Key))) == ui.KeyLabel(b.seq[0])
}

// entersMode reports the mode a binding enters: client.mode.enter{name}, or a
// Lua action named like rexkit_mode_pane / pane_mode.
func entersMode(b rex.Binding, actions map[string]rex.Action) (mode string, once bool) {
	if b.Action == "client.mode.enter" {
		var args struct {
			Name string
			Once bool
		}
		if json.Unmarshal(b.Args, &args) == nil {
			return args.Name, args.Once
		}
		return "", false
	}
	if _, ok := actions[b.Action]; !ok || isExit(b, actions) {
		return "", false
	}
	words := words(b.Action)
	i := slices.Index(words, "mode")
	switch {
	case i < 0:
		return "", false
	case i < len(words)-1:
		return strings.Join(words[i+1:], "_"), false
	case i > 0:
		return words[i-1], false
	}
	return "", false
}

// isExit reports a binding that leaves the current mode.
func isExit(b rex.Binding, actions map[string]rex.Action) bool {
	switch b.Action {
	case "client.mode.exit", "client.mode.exit_all":
		return true
	}
	if _, ok := actions[b.Action]; !ok {
		return false
	}
	w := words(b.Action)
	return slices.Contains(w, "exit") && slices.Contains(w, "mode")
}

func words(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '_' || r == '.' || r == '-' })
}

func compactJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return raw
	}
	if s := b.String(); s == "null" || s == "{}" {
		return nil
	}
	return b.Bytes()
}
