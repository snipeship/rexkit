package switcher

// escapes are where sanitize.LabelSpans wrote stand-ins (␛, <U+202E>,
// U+FFFD) into a label: byte ranges [start, end). Entries keep them next to
// each label from a terminal, so rows draw the stand-ins apart from the text
// (ui.Theme.Escaped): a pane literally titled "a<U+202E>b" must not look
// like one holding U+202E.
type escapes [][2]int

// after returns the escapes from byte i on, moved by d bytes: for a label
// cut to its tail, or given a new prefix (~/ for the home directory).
func (es escapes) after(i, d int) escapes {
	var out escapes
	for _, e := range es {
		if e[0] >= i {
			out = append(out, [2]int{e[0] + d, e[1] + d})
		}
	}
	return out
}
