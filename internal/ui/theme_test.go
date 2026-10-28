package ui

import (
	"image/color"
	"testing"

	"rexkit/internal/rextheme"
)

// themes is every Rex theme the floors must hold for: the installed app's
// (when there is one) and the built-in ones.
func themes(t *testing.T) []rextheme.Theme {
	t.Helper()
	installed, err := rextheme.Installed(rextheme.DefaultPaths())
	if err != nil {
		t.Logf("no installed Rex (%v): built-in themes only", err)
	}
	return append(installed, rextheme.Builtins()...)
}

// Every theme meets every contrast floor (theme.go's min…Contrast), in both
// appearances: the real guarantee behind "measured contrast".
func TestContrastFloors(t *testing.T) {
	for _, rt := range themes(t) {
		p := FromRex(rt).C
		floor := func(what string, fg color.Color, ratio float64, bgs map[string]color.Color) {
			t.Helper()
			for name, bg := range bgs {
				if c := Contrast(fg, bg); c < ratio-0.005 {
					t.Errorf("%s: %s on %s is %.2f:1, want ≥ %.1f:1", rt.ID, what, name, c, ratio)
				}
			}
		}
		chrome := map[string]color.Color{
			"surface": p.Surface, "selBg": p.SelBg, "field": p.Field, "tray": p.Tray, "popover": p.Popover,
		}
		floor("fgMuted", p.FgMuted, minBodyContrast, chrome)
		floor("fgSubtle", p.FgSubtle, minMetaContrast, chrome)
		floor("fgSubtle", p.FgSubtle, minCardContrast, map[string]color.Color{"surface": p.Surface, "well": p.Well})
		floor("ok", p.OK, minBodyContrast, map[string]color.Color{"surface": p.Surface, "okBg": p.OKBg})
		floor("warn", p.Warn, minBodyContrast, map[string]color.Color{"surface": p.Surface, "warnBg": p.WarnBg})
		floor("err", p.Err, minBodyContrast, map[string]color.Color{"surface": p.Surface, "errBg": p.ErrBg})
		floor("info", p.Info, minBodyContrast, map[string]color.Color{"surface": p.Surface, "infoBg": p.InfoBg})
		floor("warnOnSolid", p.WarnOnSolid, minBodyContrast, map[string]color.Color{"warnSolid": p.WarnSolid})
		floor("errOnSolid", p.ErrOnSolid, minBodyContrast, map[string]color.Color{"errSolid": p.ErrSolid})
		floor("selField", p.SelField, minSelContrast, map[string]color.Color{"field": p.Field})
		floor("fg", p.Fg, minBodyContrast, map[string]color.Color{"selField": p.SelField})
		floor("wellSelFg", p.WellSelFg, minBodyContrast, map[string]color.Color{"wellSel": p.WellSelBg})
		floor("wellSel", p.WellSelBg, minSelContrast, map[string]color.Color{"well": p.Well})
		if Contrast(p.FgSubtle, p.Surface) >= Contrast(p.FgMuted, p.Surface) {
			t.Errorf("%s: fgSubtle must be fainter than fgMuted on the card", rt.ID)
		}
	}
}

// No token of any theme is the Clear sentinel, so the stage never
// composites a real colour away.
func TestNoTokenIsClear(t *testing.T) {
	for _, rt := range themes(t) {
		p := FromRex(rt).C
		for i, c := range p.tokens() {
			if IsClear(*c) {
				t.Errorf("%s: token %d is Clear", rt.ID, i)
			}
		}
	}
	// A theme that uses Clear's exact colour gets it nudged.
	rt := rextheme.Builtin(true)
	rt.UI.Content = rextheme.Color{R: 1, G: 2, B: 3}
	if p := FromRex(rt).C; IsClear(p.Fg) {
		t.Error("a token equal to Clear was not nudged")
	}
}

// Tertiary text steps up a tier on a fill where it would fall under its
// card contrast, and stays itself on the card.
func TestOnFill(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := NewTheme(dark)
		if got := th.OnFill(th.C.SelBg).C.FgSubtle; got != th.C.FgMuted {
			t.Errorf("dark=%v: fgSubtle on the selection is %v, want fgMuted", dark, got)
		}
		if got := th.OnFill(th.C.Surface).C.FgSubtle; got != th.C.FgSubtle {
			t.Errorf("dark=%v: fgSubtle stepped up on the card", dark)
		}
		line := th.StepUp(Line{{"4m", th.Subtle}, {"name", th.Text}}, th.C.SelBg)
		if !sameColor(line[0].Style.GetForeground(), th.C.FgMuted) || !sameColor(line[1].Style.GetForeground(), th.C.Fg) {
			t.Errorf("dark=%v: StepUp moved the wrong spans", dark)
		}
	}
}
