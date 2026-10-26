package rextheme

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rexkit/internal/state"
)

// Preference keys of com.superlogical.rex that pick the theme.
const (
	keyAppearance = "Workspace.appearance" // "dark", "light" or "system"
	keyDarkTheme  = "Workspace.darkThemeID"
	keyLightTheme = "Workspace.lightThemeID"
)

// tokensPath is where an app bundle keeps Rex's themes.
const tokensPath = "Contents/Resources/SuperUI_SuperDesignTokens.bundle/Contents/Resources/themes.json"

// EnvTheme names a theme ID to use instead of the user's choice (demos,
// snapshots).
const EnvTheme = "REXKIT_REX_THEME"

// cacheName is the cache entry holding themes resolved from the app. Its
// version changes with the fields of Theme, so an old entry is not read
// with tokens missing.
const cacheName = "rextheme-v3"

// Paths are where Load looks. DefaultPaths fills them in.
type Paths struct {
	Prefs       string   // Rex's preferences (binary plist)
	GlobalPrefs string   // the global domain, for the system appearance
	Apps        []string // folders searched for Rex*.app
	Cache       state.Dirs
	NoCache     bool // do not read or write the cache
}

// DefaultPaths are the user's preference files, /Applications and
// ~/Applications, and rexkit's cache.
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	p := Paths{
		Prefs:       filepath.Join(home, "Library/Preferences/com.superlogical.rex.plist"),
		GlobalPrefs: filepath.Join(home, "Library/Preferences/.GlobalPreferences.plist"),
		Apps:        []string{"/Applications", filepath.Join(home, "Applications")},
	}
	dirs, err := state.Default()
	p.Cache, p.NoCache = dirs, err != nil
	return p
}

// Choice is the user's theme choice: which theme Rex shows now.
type Choice struct {
	ID   string // theme ID
	Dark bool   // the appearance the ID was chosen for
}

// Load returns the theme Rex is showing, from DefaultPaths. It never fails:
// whatever it cannot read falls back to the built-in Merino theme of the
// right appearance (dark when even that is unknown).
func Load() Theme {
	t, _ := LoadFrom(DefaultPaths())
	return t
}

// LoadFrom is Load with explicit paths. The error says why it fell back (the
// returned theme is still usable).
func LoadFrom(p Paths) (Theme, error) {
	choice, err := Chosen(p)
	return load(p, choice, err)
}

// ForAppearance returns the user's theme for an appearance, their dark or
// light pick whichever Rex shows now, with Load's fallbacks: what a tool
// draws with once its terminal said which appearance applies.
func ForAppearance(p Paths, dark bool) (Theme, error) {
	choice, err := chosen(p, &dark)
	return load(p, choice, err)
}

// load resolves a choice (err is the error reading it).
func load(p Paths, choice Choice, err error) (Theme, error) {
	if id := os.Getenv(EnvTheme); id != "" {
		choice.ID, err = id, nil
		if t, ok := builtins()[id]; ok {
			return t, nil
		}
	}
	if err != nil {
		return Builtin(choice.Dark), err
	}
	if isCustom(choice.ID) {
		t, err := custom(p, choice.ID)
		if err != nil {
			return Builtin(choice.Dark), err
		}
		return t, nil
	}
	t, err := find(p, choice.ID)
	if err != nil {
		if b, ok := builtins()[choice.ID]; ok {
			return b, nil
		}
		return Builtin(choice.Dark), err
	}
	return t, nil
}

// Named returns theme id as the installed app defines it, or its built-in
// copy when the app cannot be read: a fixed theme rather than the user's
// choice (snapshots). The error says why it fell back; with no built-in
// copy either, the theme is the built-in dark one.
func Named(p Paths, id string) (Theme, error) {
	t, err := find(p, id)
	if err == nil {
		return t, nil
	}
	if b, ok := builtins()[id]; ok {
		return b, err
	}
	return Builtin(true), err
}

// Chosen reads which theme the user picked. On error it still reports the
// default dark choice.
func Chosen(p Paths) (Choice, error) { return chosen(p, nil) }

// chosen is Chosen for the appearance dark, or the one Rex shows when nil.
func chosen(p Paths, dark *bool) (Choice, error) {
	choice := Choice{ID: DefaultDark, Dark: true}
	if dark != nil && !*dark {
		choice = Choice{ID: DefaultLight}
	}
	data, err := os.ReadFile(p.Prefs)
	if err != nil {
		return choice, err
	}
	prefs, err := plistStrings(data, keyAppearance, keyDarkTheme, keyLightTheme)
	if err != nil {
		return choice, err
	}
	switch {
	case dark != nil:
		choice.Dark = *dark
	case prefs[keyAppearance] == "light":
		choice.Dark = false
	case prefs[keyAppearance] == "dark":
	default: // "system", or never set
		choice.Dark = systemDark(p.GlobalPrefs)
	}
	if choice.Dark {
		choice.ID = cmp.Or(prefs[keyDarkTheme], DefaultDark)
	} else {
		choice.ID = cmp.Or(prefs[keyLightTheme], DefaultLight)
	}
	return choice, nil
}

// systemDark reports whether macOS is in dark mode (AppleInterfaceStyle is
// "Dark"; light mode leaves it unset).
func systemDark(globalPrefs string) bool {
	data, err := os.ReadFile(globalPrefs)
	if err != nil {
		return true
	}
	prefs, err := plistStrings(data, "AppleInterfaceStyle")
	if err != nil {
		return true
	}
	return strings.EqualFold(prefs["AppleInterfaceStyle"], "dark")
}

// ThemesFile finds themes.json in an installed Rex app (Rex*.app in p.Apps);
// with several, the most recently changed one.
func ThemesFile(p Paths) (string, os.FileInfo, error) {
	var best string
	var bestInfo os.FileInfo
	for _, dir := range p.Apps {
		apps, _ := filepath.Glob(filepath.Join(dir, "Rex*.app"))
		sort.Strings(apps)
		for _, app := range apps {
			path := filepath.Join(app, tokensPath)
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if bestInfo == nil || info.ModTime().After(bestInfo.ModTime()) {
				best, bestInfo = path, info
			}
		}
	}
	if bestInfo == nil {
		return "", nil, errors.New("rextheme: no Rex app with design tokens found")
	}
	return best, bestInfo, nil
}

// cacheEntry holds the themes resolved from one version of themes.json.
type cacheEntry struct {
	Path    string           `json:"path"`
	ModTime int64            `json:"mod_time"` // UnixNano
	Size    int64            `json:"size"`
	Themes  map[string]Theme `json:"themes"`
}

// find resolves theme id from the installed app, through the cache.
func find(p Paths, id string) (Theme, error) {
	path, info, err := ThemesFile(p)
	if err != nil {
		return Theme{}, err
	}
	var cache cacheEntry
	fresh := !p.NoCache
	if fresh {
		_, err := p.Cache.ReadCache(cacheName, &cache)
		fresh = err == nil && cache.Path == path && cache.ModTime == info.ModTime().UnixNano() && cache.Size == info.Size()
		if t, ok := cache.Themes[id]; fresh && ok {
			return t, nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	t, err := parseTheme(data, id)
	if err != nil {
		return Theme{}, err
	}
	if !p.NoCache {
		if !fresh {
			cache = cacheEntry{Path: path, ModTime: info.ModTime().UnixNano(), Size: info.Size()}
		}
		if cache.Themes == nil {
			cache.Themes = map[string]Theme{}
		}
		cache.Themes[id] = t
		_ = p.Cache.WriteCache(cacheName, cache) // best effort
	}
	return t, nil
}

// ErrNoTheme means themes.json has no theme with the requested ID.
var ErrNoTheme = errors.New("rextheme: no such theme")

// parseTheme finds theme id in the contents of a themes.json (an array of
// themes) and resolves it. Only that theme is decoded in full.
func parseTheme(data []byte, id string) (Theme, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return Theme{}, fmt.Errorf("rextheme: themes.json is not an array of themes")
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return Theme{}, fmt.Errorf("rextheme: themes.json: %w", err)
		}
		if !bytes.Contains(raw, []byte(`"`+id+`"`)) {
			continue
		}
		var r rawTheme
		if err := json.Unmarshal(raw, &r); err != nil {
			return Theme{}, fmt.Errorf("rextheme: theme %s: %w", id, err)
		}
		if r.ID == id {
			return r.resolve()
		}
	}
	return Theme{}, fmt.Errorf("%w: %s", ErrNoTheme, id)
}

// ParseAll resolves every theme in the contents of a themes.json, in file
// order.
func ParseAll(data []byte) ([]Theme, error) {
	var raw []rawTheme
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rextheme: themes.json: %w", err)
	}
	out := make([]Theme, 0, len(raw))
	for _, r := range raw {
		t, err := r.resolve()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Installed resolves every theme of the installed app's themes.json (see
// ThemesFile).
func Installed(p Paths) ([]Theme, error) {
	path, _, err := ThemesFile(p)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAll(data)
}
