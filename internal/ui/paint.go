package ui

import (
	"fmt"
	"image/color"
	"strings"
)

// Paint gives every cell of a rendered view an explicit background.
//
// Rex draws a floating layer without a background of its own, so any cell
// left at the terminal's default background shows the panes underneath. Paint
// starts each line with bg and puts bg back after every SGR sequence that
// resets the background (0, an empty reset, or 49), so components keep their
// own backgrounds (selection bar, chips) and everything else is opaque.
// Lines are not padded: views are already exactly w cells wide.
func Paint(content string, bg color.Color) string {
	if bg == nil || content == "" {
		return content
	}
	r, g, b, _ := bg.RGBA()
	set := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = "\x1b[" + set + "m" + repaint(line, set) + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// repaint rewrites the SGR sequences of one line so that a background reset
// becomes a switch to bg.
func repaint(line, set string) string {
	var out strings.Builder
	out.Grow(len(line) + 32)
	for {
		start := strings.Index(line, "\x1b[")
		if start < 0 {
			out.WriteString(line)
			return out.String()
		}
		end := start + 2
		for end < len(line) && (line[end] >= '0' && line[end] <= '9' || line[end] == ';' || line[end] == ':') {
			end++
		}
		out.WriteString(line[:start])
		if end < len(line) && line[end] == 'm' {
			params := line[start+2 : end]
			out.WriteString("\x1b[" + params)
			if resetsBackground(params) {
				if params == "" {
					// An empty SGR is a full reset: keep it one, or
					// "ESC[48;…m" would set the background and keep the
					// bold (or colour) of the span before it.
					out.WriteByte('0')
				}
				out.WriteByte(';')
				out.WriteString(set)
			}
			out.WriteByte('m')
			line = line[end+1:]
			continue
		}
		// Not an SGR sequence: copy the introducer and carry on after it.
		out.WriteString(line[start:end])
		line = line[end:]
	}
}

// resetsBackground reports whether an SGR parameter list leaves the
// background at the terminal default once it has been applied.
func resetsBackground(params string) bool {
	if params == "" {
		return true
	}
	fields := strings.Split(params, ";")
	reset := false
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		code := f
		if j := strings.IndexByte(f, ':'); j >= 0 {
			code = f[:j] // colon form (48:2::r:g:b) carries its own arguments
		}
		switch code {
		case "", "0", "49":
			reset = true
		case "48":
			reset = false
			fallthrough
		case "38", "58":
			if code == f && i+1 < len(fields) { // semicolon form: skip the colour arguments
				switch fields[i+1] {
				case "2":
					i += 4
				case "5":
					i += 2
				}
			}
		default:
			if n := len(code); n == 2 && code[0] == '4' && code[1] >= '0' && code[1] <= '7' {
				reset = false // 40–47
			} else if n == 3 && code[0] == '1' && code[1] == '0' && code[2] >= '0' && code[2] <= '7' {
				reset = false // 100–107
			}
		}
	}
	return reset
}
