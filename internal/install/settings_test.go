package install

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseLuaData(t *testing.T) {
	tests := []struct {
		name string
		src  string
		key  string
		want any
	}{
		{"string", `return { a = "x\ty\"" }`, "a", "x\ty\""},
		{"single quotes", `return { a = 'it''s' }`, "", nil},
		{"long string", "return { a = [==[\nx]]y]==] }", "a", "x]]y"},
		{"number", `return { a = 0.05 }`, "a", 0.05},
		{"negative hex", `return { a = -0x10 }`, "a", -16.0},
		{"bool", `return { a = false }`, "a", false},
		{"nil is declared", `return { a = nil }`, "a", nil},
		{"bracket key", `return { ["a b"] = 1 }`, "a b", 1.0},
		{"comments", "-- head\nreturn { --[[ inline ]] a = 1, -- tail\n}", "a", 1.0},
		{"via a local", "local M = { a = true }\nreturn M", "a", true},
		{"decimal escape", `return { a = "\65\066" }`, "a", "AB"},
		{"semicolons", `return { a = 1; b = 2; }`, "a", 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab, err := parseLuaData(tt.src)
			if tt.key == "" {
				if err == nil {
					t.Fatalf("parsed %q, want an error", tt.src)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, ok := tab.Get(tt.key)
			if !ok || !luaEqual(got, tt.want) {
				t.Errorf("%s = %#v (declared %v), want %#v", tt.key, got, ok, tt.want)
			}
		})
	}
}

func TestParseLuaDataRejectsCode(t *testing.T) {
	for _, src := range []string{
		`return { a = os.getenv("HOME") }`,
		`return { a = 1 } extra`,
		`print("hi")`,
		`return 42`,
		`return { a = "unfinished }`,
		`return { a = 1`,
	} {
		if _, err := parseLuaData(src); err == nil {
			t.Errorf("parseLuaData(%q) succeeded", src)
		}
	}
}

func TestFormatLuaRoundTrips(t *testing.T) {
	src := `return { editor = { "/opt/homebrew/bin/nvim", "-p" }, step = 0.05, n = 3, on = true,
		label = "a \"quoted\"\nline\x01", nested = { deep = { "x" } }, ["odd key"] = "v" }`
	want, err := parseLuaData(src)
	if err != nil {
		t.Fatal(err)
	}
	formatted := "return " + formatLua(want, 0)
	got, err := parseLuaData(formatted)
	if err != nil {
		t.Fatalf("reparsing\n%s\n: %v", formatted, err)
	}
	if !luaEqual(got, want) {
		t.Errorf("round trip changed the value:\n%s", formatted)
	}
}

func TestPlanSettings(t *testing.T) {
	tests := []struct {
		name        string
		files       []string // settings.lua, then settings.local.lua
		wantKeep    []string
		wantDropped []string
	}{
		{
			name:        "only values that differ from the defaults",
			files:       []string{`return { shell = "/opt/homebrew/bin/nu", editor = { "nvim" }, resize_step = 0.05, scratch_label = "Scratch" }`},
			wantKeep:    []string{"shell", "resize_step"},
			wantDropped: nil,
		},
		{
			name:        "unknown and retired settings are dropped",
			files:       []string{`return { python = "/usr/bin/python3", tiny_navigation = false, alt_shortcuts = false, compact_menus = true }`},
			wantKeep:    []string{"alt_shortcuts"},
			wantDropped: []string{"python", "tiny_navigation", "compact_menus"},
		},
		{
			name:     "the local file wins",
			files:    []string{`return { resize_step = 0.05 }`, `return { resize_step = 0.1, commit_via = "global" }`},
			wantKeep: []string{"commit_via"},
		},
		{
			name:  "all defaults writes no file",
			files: []string{`return { editor = { "nvim" }, compact_labels = true }`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			defaults := filepath.Join(dir, "defaults.lua")
			writeFile(t, defaults, testDefaults)
			var sources []string
			for i, src := range tt.files {
				path := filepath.Join(dir, []string{"settings.lua", "settings.local.lua"}[i])
				writeFile(t, path, src)
				sources = append(sources, path)
			}
			plan, err := planSettings(sources, defaults, testNow)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(plan.keep, tt.wantKeep) || !slices.Equal(plan.dropped, tt.wantDropped) {
				t.Errorf("keep %v dropped %v, want %v and %v", plan.keep, plan.dropped, tt.wantKeep, tt.wantDropped)
			}
			if (plan.data != nil) != (len(tt.wantKeep) > 0) {
				t.Fatalf("data = %q", plan.data)
			}
			if plan.data == nil {
				return
			}
			written, err := parseLuaData(string(plan.data))
			if err != nil {
				t.Fatalf("the written file does not parse: %v\n%s", err, plan.data)
			}
			if !slices.Equal(written.Keys, tt.wantKeep) {
				t.Errorf("written keys %v, want %v", written.Keys, tt.wantKeep)
			}
			for _, d := range tt.wantDropped {
				if !strings.Contains(string(plan.data), d) {
					t.Errorf("the file does not mention the dropped %s", d)
				}
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	w := &world{t: t}
	w.write(path, content)
}

// The checkout's own defaults must stay readable by the installer, and the
// real rex-zellij settings file (a copy in testdata) must migrate without
// errors.
func TestRepositorySettingsMigrate(t *testing.T) {
	defaults := filepath.Join("..", "..", "lua", "defaults.lua")
	sources := legacySettings(filepath.Join("testdata", "rex-zellij"))
	if len(sources) != 1 {
		t.Fatalf("sources = %v, want testdata's settings.lua", sources)
	}
	plan, err := planSettings(sources, defaults, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.dropped, "python") {
		t.Errorf("python was not dropped: keep %v, dropped %v", plan.keep, plan.dropped)
	}
}

// Settings files are written by hand: the parser must never panic, and what
// it accepts must survive formatting.
func FuzzParseLuaData(f *testing.F) {
	for _, seed := range []string{
		testDefaults,
		`return { a = "x\ty", b = { 1, 2, -3.5 }, ["c d"] = [==[e]]f]==], g = nil }`,
		"local M = { a = true } return M",
		`return { a = "\u{1F600}\x41\65\z   b" }`,
		"--[[ c ]] return { --x\n a = 0x1F; }",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		v, err := parseLuaData(src)
		if err != nil {
			return
		}
		again, err := parseLuaData("return " + formatLua(v, 0))
		if err != nil {
			t.Fatalf("formatted value does not parse: %v\n%s", err, formatLua(v, 0))
		}
		if !luaEqual(normalize(v), normalize(again)) {
			t.Fatalf("round trip changed %q", src)
		}
	})
}

// normalize drops NaN, which never equals itself.
func normalize(v any) any {
	switch v := v.(type) {
	case float64:
		if v != v {
			return "NaN"
		}
	case *luaTable:
		for i := range v.Items {
			v.Items[i] = normalize(v.Items[i])
		}
		for k, f := range v.Fields {
			v.Fields[k] = normalize(f)
		}
	}
	return v
}
