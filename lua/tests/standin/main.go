//go:build darwin

// Command standin imitates rexkit for live tests of the Lua layer: tools
// answer the handshake keys, `await` waits for result.json, `hold` idles.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// server is the isolated server the stand-in talks to. A Rex server gives its
// blocks no REX_SERVER of its own; the test setup exports it to the server,
// and its blocks inherit it. Without it every call would go to the default
// (the user's) server, so the stand-in refuses to run.
var server = os.Getenv("REX_SERVER")

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	if server == "" && os.Args[1] != "toast" {
		fmt.Fprintln(os.Stderr, "standin: REX_SERVER unset; refusing to talk to the default server")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "await":
		await(os.Args[2:])
	case "hold":
		record(os.Args)
		signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
		end := make(chan os.Signal, 1) // like rexkit hold: only SIGTERM or SIGHUP end it
		signal.Notify(end, syscall.SIGTERM, syscall.SIGHUP)
		<-end
	case "toast":
		record(os.Args)
		return
	case "attend":
		record(os.Args)
		attend(os.Args[2:])
	default:
		tool(os.Args[1], os.Args[2:])
	}
}

func record(argv []string) {
	if log := os.Getenv("STANDIN_LOG"); log != "" {
		f, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		b, _ := json.Marshal(argv)
		fmt.Fprintf(f, "%s\n", b)
		f.Close()
	}
}

// attend plays `rexkit attend`: the watcher (--token) only logs its argv;
// next and back print the jump in jump.json beside $STANDIN_LOG, or "No
// agent needs you".
func attend(args []string) {
	if len(args) == 0 || (args[0] != "next" && args[0] != "back") {
		return
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv("STANDIN_LOG")), "jump.json"))
	if err != nil {
		b = []byte(`{"toast":{"message":"No agent needs you","kind":"ok"}}`)
	}
	os.Stdout.Write(b)
}

func await(args []string) {
	fs := flag.NewFlagSet("await", flag.ExitOnError)
	dir := fs.String("state", "", "")
	timeout := fs.Duration("timeout", 1500*time.Millisecond, "")
	_ = fs.Parse(args)
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(*dir, "result.json")); err == nil {
			os.Stdout.Write(b)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(3)
}

type opts struct {
	session, block, window, state string
	zoomed                        bool
}

func rex(args ...string) ([]byte, error) {
	bin := filepath.Join(os.Getenv("REX_BIN_DIR"), "rex")
	return exec.Command(bin, append([]string{"--autostart=false", "-S", server}, args...)...).Output()
}

func call(method string, v any) []byte {
	b, _ := json.Marshal(v)
	out, _ := rex("api", "call", method, string(b))
	return out
}

func raw() {
	var t syscall.Termios
	fd := uintptr(0)
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	t.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
	t.Iflag &^= syscall.ICRNL
	syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&t)))
}

func tool(name string, args []string) {
	record(os.Args)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var o opts
	fs.StringVar(&o.session, "session", "", "")
	fs.StringVar(&o.block, "origin-block", "", "")
	fs.StringVar(&o.window, "origin-window", "", "")
	fs.StringVar(&o.state, "state", "", "")
	fs.BoolVar(&o.zoomed, "zoomed", false, "")
	fs.String("client", "", "")
	fs.Bool("dismiss-on-blur", true, "")
	for _, f := range []string{"shell", "editor-json", "directory", "start", "target", "id", "current", "mode", "card", "card-cells", "backdrop",
		"page", "line", "search", "next-key", "first-json", "query"} {
		fs.String(f, "", "")
	}
	fs.Bool("regex", false, "")
	fs.Bool("match-case", false, "")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	self := os.Getenv("REX_BLOCK")
	write(o.state, "owner.json", map[string]string{"session_id": o.session, "block_id": self, "server": server})
	raw()
	fmt.Print("standin ", name, " ready\r\n")
	typed := ""
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		// hints: ⇥, which reaches it as a key (it runs with no mode)
		if name == "hints" && string(buf[:n]) == "\t" {
			record([]string{"read", "tab"})
			continue
		}
		// search: F1, which Lua writes into it (rexkit_keys)
		if name == "search" && string(buf[:n]) == "\x1bOP" {
			record([]string{"read", "f1"})
			continue
		}
		for _, c := range buf[:n] {
			if c == '\r' || c == 0x1b || c == 0x03 {
				// typed "slow" first: answer the next key after 300ms, so a
				// second key lands while Lua still waits for this one
				if rest, ok := strings.CutPrefix(typed, "slow"); ok {
					time.Sleep(300 * time.Millisecond)
					typed = rest
				}
			}
			switch c {
			case 0x06:
				// ⌃F: Go to's goes with the commit marker and hands what was
				// typed to Search; any other tool reads it as a key
				commit := os.Remove(filepath.Join(o.state, "commit")) == nil
				if name != "goto" {
					record([]string{"read", "ctrl+f", fmt.Sprint(commit)})
					continue
				}
				write(o.state, "result.json", map[string]any{"seq": time.Now().UnixNano(), "action": "invoke",
					"invoke": "rexkit_search", "args": map[string]string{"query": typed, "after": self}, "commit": commit})
				closeSelf(o, self)
				return
			case '\r':
				// like popup.TakeCommit: the ⏎ Lua's handshake writes comes
				// with a one-shot marker; the result says whether it was there
				commit := os.Remove(filepath.Join(o.state, "commit")) == nil
				if typed == "stay" {
					write(o.state, "result.json", map[string]any{"seq": time.Now().UnixNano(), "action": "stay", "commit": commit})
					typed = ""
					continue
				}
				res := map[string]any{"seq": time.Now().UnixNano(), "action": "goto", "session_id": o.session, "window_id": o.window, "block_id": o.block, "commit": commit}
				if f := strings.Fields(typed); len(f) == 4 && f[0] == "goto" {
					res["session_id"], res["window_id"], res["block_id"] = f[1], f[2], f[3]
				}
				if strings.HasPrefix(typed, "invoke ") {
					res["invoke"] = strings.TrimPrefix(typed, "invoke ")
				}
				if f := strings.Fields(typed); len(f) == 5 && f[0] == "jump" {
					res["session_id"], res["window_id"], res["block_id"] = f[1], f[2], f[3]
					res["invoke"], res["args"] = "rexkit_search_jump", jump(o, f[1:])
				}
				write(o.state, "result.json", res)
				closeSelf(o, self)
				return
			case 0x1b:
				if typed == "back" {
					write(o.state, "result.json", map[string]any{"seq": time.Now().UnixNano(), "action": "stay"})
					typed = ""
					continue
				}
				fallthrough
			case 0x03:
				write(o.state, "result.json", map[string]any{"seq": time.Now().UnixNano(), "action": "close"})
				closeSelf(o, self)
				return
			default:
				typed += string(c)
			}
		}
	}
}

// jump plays Search's ⏎ on a match (`jump S W B LINE`): the capture goes
// into a folder of its own beside the tool's, and rexkit_search_jump gets
// the pane, the page, the line and the query "needle".
func jump(o opts, f []string) map[string]any {
	dir, _ := os.MkdirTemp(filepath.Dir(o.state), "page")
	page := filepath.Join(dir, "page.txt")
	os.WriteFile(page, []byte("a needle\n"), 0o600)
	line := 0
	fmt.Sscan(f[3], &line)
	return map[string]any{"session_id": f[0], "window_id": f[1], "block_id": f[2], "page": page, "line": line,
		"search": "needle", "match_case": true}
}

func write(dir, name string, v any) {
	b, _ := json.Marshal(v)
	tmp := filepath.Join(dir, name+".tmp")
	os.WriteFile(tmp, b, 0o600)
	os.Rename(tmp, filepath.Join(dir, name))
}

func closeSelf(o opts, self string) {
	os.Remove(filepath.Join(o.state, "owner.json"))
	var view struct {
		Windows []struct {
			Layers []struct {
				LayerID string `json:"layer_id"`
				Blocks  []struct {
					BlockID string `json:"block_id"`
				} `json:"blocks"`
			} `json:"layers"`
		} `json:"windows"`
	}
	json.Unmarshal(call("session.view", map[string]string{"session_id": o.session}), &view)
	if o.zoomed {
		call("session.zoom_block", map[string]any{"session_id": o.session, "block_id": o.block, "zoom": true})
	}
	signal.Ignore(syscall.SIGHUP)
	for _, w := range view.Windows {
		for _, l := range w.Layers {
			for _, b := range l.Blocks {
				if b.BlockID == self {
					call("session.close_layer", map[string]string{"session_id": o.session, "layer_id": l.LayerID})
				}
			}
		}
	}
}
