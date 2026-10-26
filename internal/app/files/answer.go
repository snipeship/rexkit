package files

import "rexkit/internal/popup"

// answer writes a bare result for the awaiting Lua layer (the handshake in
// popup/result.go): Lua writes ⏎ or ⎋ into Files and waits for result.json.
// A verb that opens a terminal answers with its result; popup.ActionStay and
// popup.ActionClose cover the other outcomes. Without a state dir (a demo,
// or a run by hand) nobody waits and nothing is written.
func (l launcher) answer(action string) {
	_ = popup.Answer(l.stateDir, action)
}

// stay answers a handshake key that Files used without closing.
func (m *model) stay(key string) {
	if key == "enter" || key == "esc" {
		m.cfg.launcher.answer(popup.ActionStay)
	}
}
