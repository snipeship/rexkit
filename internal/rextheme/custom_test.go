package rextheme

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// derive gives every generated theme the installed app ships (all but the
// hand-made ones) the tokens Rex gave it, to within a few 1/255.
func TestDeriveBundled(t *testing.T) {
	path, _, err := ThemesFile(DefaultPaths())
	if err != nil {
		t.Skipf("no installed Rex: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raws []rawTheme
	if err := json.Unmarshal(data, &raws); err != nil {
		t.Fatal(err)
	}
	// Rex generates the tokens from the Display P3 source colours, which
	// may differ from the hex ones, and rexkit reads every token's
	// components as they are; so derive works from those, as it does for a
	// custom theme's palette.
	var p3 []struct {
		Source struct {
			DisplayP3 struct {
				Background string            `json:"background"`
				Foreground string            `json:"foreground"`
				Cursor     string            `json:"cursor"`
				ANSI       map[string]string `json:"ansi"`
			} `json:"displayP3"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &p3); err != nil {
		t.Fatal(err)
	}
	handMade := map[string]bool{"merino-dark": true, "merino-light": true, "silver-point-dark": true, "silver-point-light": true}
	worst := map[string]int{}
	for i, r := range raws {
		if handMade[r.ID] {
			continue
		}
		want, err := r.resolve()
		if err != nil {
			t.Fatal(err)
		}
		src := p3[i].Source.DisplayP3
		var ansi [16]Color
		for j := range ansi {
			ansi[j] = p3Color(t, src.ANSI[fmt.Sprint(j)])
		}
		d := r
		d.Semantic = derive(want.Dark, p3Color(t, src.Background), p3Color(t, src.Foreground), p3Color(t, src.Cursor), ansi)
		got, err := d.resolve()
		if err != nil {
			t.Fatal(err)
		}
		for name, diff := range uiDiff(got.UI, want.UI) {
			if diff > worst[name] {
				worst[name] = diff
			}
			if limit := tolerance(name); diff > limit {
				t.Errorf("%s: %s is %d/255 off (allowed %d)", r.ID, name, diff, limit)
			}
		}
	}
	t.Logf("worst difference per token: %v", worst)
}

// p3Color reads "color(display-p3 r g b)" as a token's components.
func p3Color(t *testing.T, s string) Color {
	t.Helper()
	var c tokenColor
	if _, err := fmt.Sscanf(s, "color(display-p3 %g %g %g)", &c.Red, &c.Green, &c.Blue); err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return c.rgba().Color
}

// tolerance is how far (in 1/255 of a channel) a derived token may be from
// Rex's. Surfaces, strokes and text must be close (they are within 5); the
// accent and status colours, which Rex nudges for contrast in a colour space
// of its own, are within 40.
func tolerance(name string) int {
	switch name {
	case "Canvas", "Surface", "SurfaceSubtle", "Stroke", "StrokeSubtle", "Content", "ContentSecondary", "ContentTertiary", "Control", "ControlHover", "ControlPressed":
		return 6
	case "SurfaceRaised", "ControlOnOverlay":
		return 10
	}
	return 40
}

// uiDiff is the largest channel difference of each colour of two UIs.
func uiDiff(a, b UI) map[string]int {
	out := map[string]int{}
	var walk func(prefix string, x, y reflect.Value)
	walk = func(prefix string, x, y reflect.Value) {
		switch v := x.Interface().(type) {
		case Color:
			w := y.Interface().(Color)
			out[prefix] = max(absDiff(v.R, w.R), absDiff(v.G, w.G), absDiff(v.B, w.B))
			return
		case Alpha:
			return // compared once composited, as the opaque fields
		}
		if x.Kind() != reflect.Struct {
			return
		}
		for i := range x.NumField() {
			name := x.Type().Field(i).Name
			if prefix != "" {
				name = prefix + "." + name
			}
			walk(name, x.Field(i), y.Field(i))
		}
	}
	walk("", reflect.ValueOf(a), reflect.ValueOf(b))
	return out
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestCustom(t *testing.T) {
	p := fixture(t)
	p.Prefs = "testdata/custom-prefs.plist"
	got, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "custom-TEST" || got.Name != "tokyonight" || !got.Dark {
		t.Fatalf("loaded %s %q (dark %v)", got.ID, got.Name, got.Dark)
	}
	term, ui := got.Terminal, got.UI
	if term.Background != (Color{}) || term.Foreground != (Color{255, 255, 255}) || term.ANSI[4] != (Color{0x7a, 0xa2, 0xf7}) {
		t.Errorf("terminal: bg %s fg %s blue %s", term.Background, term.Foreground, term.ANSI[4])
	}
	if term.Palette[200] != (Color{0xff, 0x00, 0xd7}) || term.Palette[17] != (Color{0x01, 0x02, 0x03}) {
		t.Errorf("paletteBase: 200 %s, 17 %s", term.Palette[200], term.Palette[17])
	}
	// The canvas is the background and surfaces rise from it towards the
	// foreground; a white cursor gives way to ANSI blue as the accent.
	if ui.Canvas != term.Background || ui.SurfaceRaised.Luma() <= ui.Surface.Luma() || ui.Surface.Luma() < ui.Canvas.Luma() {
		t.Errorf("canvas %s surface %s raised %s", ui.Canvas, ui.Surface, ui.SurfaceRaised)
	}
	if ui.Accent != term.ANSI[4] || ui.AccentSolid != term.ANSI[4] || ui.AccentOnSolid != term.Background {
		t.Errorf("accent %s solid %s on it %s", ui.Accent, ui.AccentSolid, ui.AccentOnSolid)
	}
	if ui.Danger.Color != term.ANSI[1] || ui.Success.Color != term.ANSI[2] {
		t.Errorf("danger %s success %s", ui.Danger.Color, ui.Success.Color)
	}

	for _, id := range []string{"custom-NOPE", "custom-BROKEN"} {
		t.Setenv(EnvTheme, id)
		if got, err := LoadFrom(p); err == nil || got.ID != DefaultDark {
			t.Errorf("%s: %s, %v; want the built-in dark theme and an error", id, got.ID, err)
		}
	}
}

// A text colour too dark to read on the surface is lifted until it can be.
func TestDeriveContrast(t *testing.T) {
	bg, fg := Color{0x10, 0x10, 0x10}, Color{0xdd, 0xdd, 0xdd}
	var ansi [16]Color
	for i := range ansi {
		ansi[i] = Color{0x40, 0x10, 0x10}
	}
	m := derive(true, bg, fg, Color{0x30, 0x10, 0x60}, ansi)
	surface := m["surface"].rgba().Color
	for _, name := range []string{"accent", "danger", "info"} {
		c := m[name].rgba().Color
		if r := contrast(c, surface); r < readableContrast {
			t.Errorf("%s %s on %s: contrast %.2f", name, c, surface, r)
		}
		if solid := m[name+"Solid"].rgba().Color; solid == c {
			t.Errorf("%s: the solid was lifted too (%s)", name, solid)
		}
	}
	if got := fmt.Sprint(m["accentSolid"].rgba().Color); got != "#301060" {
		t.Errorf("accentSolid %s, want the cursor", got)
	}
}
