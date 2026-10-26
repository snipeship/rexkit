package rex

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Error kinds. Test with errors.Is.
var (
	// ErrNotFound means a session, window, layer or block does not exist (any more).
	ErrNotFound = errors.New("not found")
	// ErrRefused means the app refused a client action because Remote Control is off.
	ErrRefused = errors.New("refused")
	// ErrUnavailable means the Rex server could not be reached.
	ErrUnavailable = errors.New("rex server unavailable")
)

// Error is a failed rex call with a clean, single-line message.
type Error struct {
	Op      string // API method or CLI command
	Status  int    // HTTP-like status, 0 when unknown
	Code    string // server error code such as not_found, empty when unknown
	Message string
	kind    error
}

func (e *Error) Error() string {
	if e.Op == "" {
		return e.Message
	}
	return e.Op + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.kind }

// exitUnavailable is the CLI's exit status when it cannot connect.
const exitUnavailable = 4

var (
	statusSuffix = regexp.MustCompile(`\s*\(status=(\d+) code=([a-z_]+)\)\s*$`)
	noisePrefix  = regexp.MustCompile(`^(Error:\s*)?(stream response "[^"]*" failed:\s*)?(netapi:\s*)?`)
)

// parseError turns the CLI's stderr into an *Error.
func parseError(op string, exitCode int, stderr string) *Error {
	e := &Error{Op: op}
	msg := lastErrorLine(stderr)
	if m := statusSuffix.FindStringSubmatch(msg); m != nil {
		e.Status, _ = strconv.Atoi(m[1])
		e.Code = m[2]
		msg = msg[:len(msg)-len(m[0])]
	}
	msg = noisePrefix.ReplaceAllString(msg, "")
	e.Message = cleanText(msg, 300)
	if e.Message == "" {
		e.Message = "rex exited with status " + strconv.Itoa(exitCode)
	}
	e.kind = classify(exitCode, e.Status, e.Code, e.Message)
	return e
}

// UserMessage is err as one clean line for a toast or an error card: the
// server's message without the RPC method ("window limit reached", not
// "session.new_window: window limit reached"), a plain phrase for a missing
// target, an unreachable server or a timeout, and any context the caller
// wrapped around the error kept ("couldn't copy: …"). nil gives "".
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	full := err.Error()
	var e *Error
	if !errors.As(err, &e) {
		if errors.Is(err, context.DeadlineExceeded) {
			return "Rex did not answer in time"
		}
		return CleanMessage(full)
	}
	msg := e.Message
	switch {
	case errors.Is(e, ErrNotFound):
		msg = "that target is gone"
	case errors.Is(e, ErrUnavailable):
		msg = "Rex is not reachable"
	}
	if inner := e.Error(); inner != full {
		msg = strings.Replace(full, inner, msg, 1) // keep what the caller added
	}
	return cleanText(msg, 300)
}

// CleanMessage strips transport noise from an error text reported by Rex,
// such as the err string a Lua rex.call returns.
func CleanMessage(s string) string { return parseError("", 0, s).Message }

func classify(exitCode, status int, code, msg string) error {
	switch {
	case code == "refused" || status == 403:
		return ErrRefused
	case strings.HasSuffix(code, "not_found") && code != "method_not_found":
		return ErrNotFound
	case status == 0 && strings.Contains(msg, "target") && strings.HasSuffix(msg, "not found"):
		return ErrNotFound // CLI-side resolution of -s/-b
	case exitCode == exitUnavailable || strings.HasPrefix(msg, "connecting to Rex server"):
		return ErrUnavailable
	}
	return nil
}

// lastErrorLine picks the line that carries the error, ignoring blank lines
// and progress chatter.
func lastErrorLine(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "Error:") {
			return lines[i]
		}
	}
	return lines[len(lines)-1]
}

// cleanText makes s a single printable line of at most limit runes.
func cleanText(s string, limit int) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range ansi.Strip(s) {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
			n++
		}
		if n >= limit {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
