package ui

import (
	"image/color"
	"testing"
)

func TestPaint(t *testing.T) {
	bg := color.RGBA{0x16, 0x1a, 0x21, 0xff}
	const set = "\x1b[48;2;22;26;33m"
	tests := []struct{ name, in, want string }{
		{"plain", "ab", set + "ab\x1b[0m"},
		{"reset", "a\x1b[0mb", set + "a\x1b[0;48;2;22;26;33mb\x1b[0m"},
		{"empty reset", "a\x1b[mb", set + "a\x1b[0;48;2;22;26;33mb\x1b[0m"},
		{"default bg", "\x1b[1;49mx", set + "\x1b[1;49;48;2;22;26;33mx\x1b[0m"},
		{"own bg kept", "\x1b[0;48;2;38;53;77mx", set + "\x1b[0;48;2;38;53;77mx\x1b[0m"},
		{"rgb zero is not a reset", "\x1b[38;2;0;0;0mx", set + "\x1b[38;2;0;0;0mx\x1b[0m"},
		{"256 colour bg kept", "\x1b[0;48;5;0mx", set + "\x1b[0;48;5;0mx\x1b[0m"},
		{"colon bg kept", "\x1b[0;48:2::1:2:3mx", set + "\x1b[0;48:2::1:2:3mx\x1b[0m"},
		{"basic bg kept", "\x1b[0;44mx", set + "\x1b[0;44mx\x1b[0m"},
		{"non-SGR untouched", "\x1b[2Kx", set + "\x1b[2Kx\x1b[0m"},
		{"lines", "a\nb", set + "a\x1b[0m\n" + set + "b\x1b[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Paint(tt.in, bg); got != tt.want {
				t.Errorf("Paint(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
