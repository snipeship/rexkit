package rextheme

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rexkit/internal/state"
)

func fixture(t testing.TB) Paths {
	t.Helper()
	t.Setenv(EnvTheme, "")
	dir := t.TempDir()
	return Paths{
		Prefs:       "testdata/prefs.plist",
		GlobalPrefs: "testdata/global-dark.plist",
		Apps:        []string{"testdata/Applications"},
		Cache:       state.Dirs{Cache: dir},
	}
}

func TestPlistStrings(t *testing.T) {
	data, err := os.ReadFile("testdata/prefs.plist")
	if err != nil {
		t.Fatal(err)
	}
	got, err := plistStrings(data, keyAppearance, keyLightTheme, "Workspace.terminalFontName", "Workspace.fontSize", "Workspace.flags", "missing")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		keyAppearance:                "light",
		keyLightTheme:                "test-light",
		"Workspace.terminalFontName": "Bérkeley Mono ✓", // UTF-16 in the plist
	}
	if len(got) != len(want) {
		t.Errorf("got %q, want %q", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, bad := range [][]byte{nil, []byte("bplist00"), []byte("<?xml version=\"1.0\"?><plist/>"), data[:len(data)-1]} {
		if _, err := plistStrings(bad, keyAppearance); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestChosen(t *testing.T) {
	p := fixture(t)
	c, err := Chosen(p)
	if err != nil || c != (Choice{ID: "test-light", Dark: false}) {
		t.Errorf("Chosen = %+v, %v; want test-light", c, err)
	}
	p.Prefs = filepath.Join(t.TempDir(), "missing.plist")
	if c, err := Chosen(p); err == nil || c != (Choice{ID: DefaultDark, Dark: true}) {
		t.Errorf("missing prefs: %+v, %v", c, err)
	}
	if !systemDark("testdata/global-dark.plist") || systemDark("testdata/prefs.plist") {
		t.Error("systemDark misread AppleInterfaceStyle")
	}
}

func TestLoadFrom(t *testing.T) {
	p := fixture(t)
	got, err := LoadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "test-light" || got.Dark {
		t.Fatalf("loaded %s (dark %v), want test-light", got.ID, got.Dark)
	}
	if got.Terminal.Background != (Color{0xff, 0xff, 0xff}) || got.UI.Canvas != (Color{230, 230, 230}) {
		t.Errorf("bg %s canvas %s", got.Terminal.Background, got.UI.Canvas)
	}
	// Missing tokens are derived, never left black.
	if got.UI.Surface != got.Terminal.Background || got.UI.Content != got.Terminal.Foreground {
		t.Errorf("surface %s content %s", got.UI.Surface, got.UI.Content)
	}

	// The cache now holds it; a changed themes.json invalidates the cache.
	var cache cacheEntry
	if _, err := p.Cache.ReadCache(cacheName, &cache); err != nil || cache.Themes["test-light"].ID != "test-light" {
		t.Fatalf("cache: %+v, %v", cache, err)
	}
	cache.Themes["test-light"] = Theme{ID: "test-light", Name: "From the cache"}
	if err := p.Cache.WriteCache(cacheName, cache); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadFrom(p); got.Name != "From the cache" {
		t.Errorf("cache not used: %s", got.Name)
	}
	path, info, _ := ThemesFile(p)
	if err := os.Chtimes(path, time.Now(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	defer os.Chtimes(path, time.Now(), info.ModTime())
	if got, _ := LoadFrom(p); got.Name != "Test Light" {
		t.Errorf("stale cache used: %s", got.Name)
	}
}

func TestLoadFallbacks(t *testing.T) {
	p := fixture(t)
	p.Apps = []string{t.TempDir()}
	got, err := LoadFrom(p)
	if err == nil || got.ID != DefaultLight {
		t.Errorf("no app: %s, %v; want the built-in light theme and an error", got.ID, err)
	}
	t.Setenv(EnvTheme, DefaultDark)
	if got, err := LoadFrom(p); err != nil || got.ID != DefaultDark {
		t.Errorf("%s=%s: %s, %v", EnvTheme, DefaultDark, got.ID, err)
	}
	p = fixture(t)
	t.Setenv(EnvTheme, "test-dark")
	got, err = LoadFrom(p)
	if err != nil || got.ID != "test-dark" || !got.Dark {
		t.Fatalf("%s=test-dark: %s, %v", EnvTheme, got.ID, err)
	}
	t.Setenv(EnvTheme, "nope")
	if got, err := LoadFrom(p); !errors.Is(err, ErrNoTheme) || got.ID != DefaultLight {
		t.Errorf("unknown theme: %s, %v", got.ID, err)
	}
}

func TestNamed(t *testing.T) {
	p := fixture(t)
	if got, err := Named(p, "test-dark"); err != nil || got.ID != "test-dark" {
		t.Errorf("from the app: %s, %v", got.ID, err)
	}
	if got, err := Named(p, DefaultLight); err == nil || got.ID != DefaultLight {
		t.Errorf("not in the app: %s, %v; want the built-in copy and an error", got.ID, err)
	}
	if got, err := Named(p, "nope"); !errors.Is(err, ErrNoTheme) || got.ID != DefaultDark {
		t.Errorf("unknown theme: %s, %v", got.ID, err)
	}
}

func TestResolve(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata/Applications/Rex Test.app", tokensPath))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseTheme(data, "test-dark")
	if err != nil {
		t.Fatal(err)
	}
	ui := got.UI
	surface := Color{51, 51, 51}
	if ui.Surface != surface || ui.Canvas != (Color{}) {
		t.Errorf("surface %s canvas %s", ui.Surface, ui.Canvas)
	}
	// accentDim is the accent at 50% over the surface.
	if want := (Color{26, 77, 153}); ui.AccentDim != want {
		t.Errorf("accentDim = %s, want %s", ui.AccentDim, want)
	}
	if ui.Alpha.AccentDim.A != 0.5 || ui.Alpha.AccentDim.Color != (Color{0, 102, 255}) {
		t.Errorf("raw accentDim = %+v", ui.Alpha.AccentDim)
	}
	if ui.Danger.Color != (Color{255, 0, 0}) || ui.Danger.Container != (Color{102, 0, 0}) || ui.Danger.OnContainer != ui.Danger.Color {
		t.Errorf("danger = %+v", ui.Danger)
	}
	// Without a palette, 16–255 are the xterm cube and greys.
	term := got.Terminal
	if term.Indexed(1) != term.ANSI[1] || term.Indexed(196) != (Color{255, 0, 0}) || term.Indexed(232) != (Color{8, 8, 8}) {
		t.Errorf("palette: 1=%s 196=%s 232=%s", term.Indexed(1), term.Indexed(196), term.Indexed(232))
	}
	if _, err := parseTheme(data, "missing"); !errors.Is(err, ErrNoTheme) {
		t.Errorf("missing theme: %v", err)
	}
	if _, err := parseTheme([]byte(`{"id":"x"}`), "x"); err == nil {
		t.Error("an object is not a list of themes")
	}
}

func TestBuiltin(t *testing.T) {
	dark, light := Builtin(true), Builtin(false)
	if dark.ID != DefaultDark || !dark.Dark || light.ID != DefaultLight || light.Dark {
		t.Fatalf("builtins: %s %v, %s %v", dark.ID, dark.Dark, light.ID, light.Dark)
	}
	if got := dark.Terminal.Background.Hex(); got != "#0d1117" {
		t.Errorf("merino-dark background = %s", got)
	}
	// Rex's generated palette, not the xterm cube: 16 is the background.
	if dark.Terminal.Indexed(16) != dark.Terminal.Background {
		t.Errorf("merino-dark 16 = %s", dark.Terminal.Indexed(16))
	}
	if dark.UI.Canvas.Luma() >= dark.UI.Content.Luma() || light.UI.Canvas.Luma() <= light.UI.Content.Luma() {
		t.Error("content does not stand out from the canvas")
	}
}

func TestColor(t *testing.T) {
	c, err := ParseHex("#3080ff")
	if err != nil || c != (Color{0x30, 0x80, 0xff}) || c.Hex() != "#3080ff" {
		t.Errorf("ParseHex = %v, %v", c, err)
	}
	if c, _ := ParseHex("#fff"); c != (Color{255, 255, 255}) {
		t.Errorf("short hex = %v", c)
	}
	for _, bad := range []string{"", "#12345", "#gggggg"} {
		if _, err := ParseHex(bad); err == nil {
			t.Errorf("ParseHex(%q): no error", bad)
		}
	}
	black, white := Color{}, Color{255, 255, 255}
	if got := black.Mix(white, 0.5); got != (Color{128, 128, 128}) {
		t.Errorf("Mix = %v", got)
	}
	if got := (Color{255, 0, 0}).Desaturate(1); got.R != got.G || got.G != got.B {
		t.Errorf("Desaturate(1) = %v, want a grey", got)
	}
	if got := Of(Color{1, 2, 3}); got != (Color{1, 2, 3}) {
		t.Errorf("Of = %v", got)
	}
	var back Color
	text, _ := c.MarshalText()
	if err := back.UnmarshalText(text); err != nil || back != c {
		t.Errorf("text round trip: %v, %v", back, err)
	}
}

func BenchmarkLoadCached(b *testing.B) {
	p := fixture(b)
	if _, err := LoadFrom(p); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		_, _ = LoadFrom(p)
	}
}

// Every theme the installed app ships resolves, with the tokens rexkit reads
// that every theme defines (none falls back to its default).
func TestInstalledThemes(t *testing.T) {
	themes, err := Installed(DefaultPaths())
	if err != nil {
		t.Skipf("no installed Rex: %v", err)
	}
	if len(themes) == 0 {
		t.Fatal("themes.json lists no themes")
	}
	for _, th := range append(themes, Builtins()...) {
		u := th.UI
		if u.Alpha.ControlPressed.A <= 0 || u.Alpha.AccentDim.A <= 0 {
			t.Errorf("%s: controlPressed %v accentDim %v", th.ID, u.Alpha.ControlPressed, u.Alpha.AccentDim)
		}
		if u.Danger.Solid == u.Danger.OnSolid || u.Warning.Solid == u.Warning.OnSolid || u.AccentSolid == u.AccentOnSolid {
			t.Errorf("%s: a solid fill and its text are the same colour", th.ID)
		}
	}
}
