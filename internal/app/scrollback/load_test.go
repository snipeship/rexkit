package scrollback

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"rexkit/internal/ui"
)

// screen is a fake terminal: keys go in through a pipe, and whatever the
// program draws is collected (safely, as Bubble Tea draws from goroutines).
type screen struct {
	in  *os.File // a real pipe, so programs can stop reading it (as with a tty)
	key *os.File

	mu  sync.Mutex
	out bytes.Buffer
}

func newScreen(t *testing.T) *screen {
	s := &screen{}
	var err error
	if s.in, s.key, err = os.Pipe(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.key.Close(); s.in.Close() })
	return s
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
}

func (s *screen) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

// press types text once the screen shows want.
func (s *screen) press(t *testing.T, want, text string) {
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(s.String(), want) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		_, _ = s.key.Write([]byte(text))
	}()
}

func runLoad(t *testing.T, s *screen, j job) (prepared, ui.Theme, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return load(ctx, j, s.in, s, tea.WithWindowSize(60, 12))
}

func TestLoadWaitsForTheColourScheme(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "")
	tests := []struct {
		reply string
		dark  bool
	}{
		{"\x1b[?997;2n", false},
		{"\x1b[?997;1n", true},
		{"", true}, // no answer: dark after the timeout
	}
	for _, tt := range tests {
		s := newScreen(t)
		if tt.reply != "" {
			s.press(t, "", tt.reply)
		}
		got, theme, err := runLoad(t, s, func(context.Context) (prepared, error) {
			return prepared{file: "/spool/scrollback.txt"}, nil
		})
		if err != nil || got.file != "/spool/scrollback.txt" || theme.Dark != tt.dark {
			t.Errorf("reply %q: %+v dark=%v err=%v", tt.reply, got, theme.Dark, err)
		}
		if out := s.String(); !strings.Contains(out, "\x1b[?2031l") {
			t.Errorf("colour-scheme reports were left on: %q", out)
		}
	}
}

func TestLoadShowsErrorsUntilAKey(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	s := newScreen(t)
	boom := errors.New("reading the pane: capture: block not found")
	s.press(t, "block not found", "x")
	_, _, err := runLoad(t, s, failed(boom))
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	out := s.String()
	for _, want := range []string{"Can't show the scrollback", "Reading the pane", "close"} {
		if !strings.Contains(out, want) {
			t.Errorf("error card lacks %q", want)
		}
	}
}

func TestLoadCanBeCancelled(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	s := newScreen(t)
	stopped := make(chan struct{})
	s.press(t, "", "q")
	_, _, err := runLoad(t, s, func(ctx context.Context) (prepared, error) {
		<-ctx.Done()
		close(stopped)
		return prepared{}, ctx.Err()
	})
	if !errors.Is(err, errCancelled) {
		t.Errorf("err = %v, want errCancelled", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Error("the capture kept running after the user closed the tool")
	}
}

func TestSlowLoadsShowASpinner(t *testing.T) {
	t.Setenv(ui.ThemeEnv, "dark")
	s := newScreen(t)
	_, _, err := runLoad(t, s, func(context.Context) (prepared, error) {
		time.Sleep(ui.SpinnerDelay + 150*time.Millisecond)
		return prepared{}, nil
	})
	if err != nil || !strings.Contains(s.String(), "Reading scrollback") {
		t.Errorf("err %v, screen %q", err, s.String())
	}
}

func TestErrorCardFitsNarrowFloats(t *testing.T) {
	long := errors.New(strings.Repeat("very long reason ", 40))
	for _, w := range []int{0, 20, 40, 120} {
		lines := strings.Split(errorCard(ui.NewTheme(true), long, w), "\n")
		if len(lines) > 11 { // lips, air, the head, 4 lines of detail, a gap, the keys, air
			t.Errorf("w=%d: %d lines", w, len(lines))
		}
		for _, l := range lines {
			if w > 0 && ansi.StringWidth(l) > max(w, 16) {
				t.Errorf("w=%d: line %q is %d wide", w, l, ansi.StringWidth(l))
			}
		}
	}
}
