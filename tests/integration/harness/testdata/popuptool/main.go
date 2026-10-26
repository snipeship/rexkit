// Command popuptool is a minimal floating tool for harness tests: it claims
// its state dir, reads one line, publishes it as its result, closes its own
// layer with popup.Close and records that Close returned.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"rexkit/internal/popup"
	"rexkit/internal/rex"
)

func main() {
	var opts popup.Options
	fs := flag.NewFlagSet("popuptool", flag.ExitOnError)
	opts.RegisterFlags(fs)
	marker := fs.String("marker", "", "file to write once Close returns")
	_ = fs.Parse(os.Args[1:])

	report := func(format string, args ...any) {
		_ = os.WriteFile(*marker, []byte(fmt.Sprintf(format, args...)), 0o600)
	}
	client, err := rex.New()
	if err != nil {
		report("rex: %v", err)
		return
	}
	ctx := context.Background()
	p := popup.New(client, opts)
	defer p.Guard()
	self, err := p.Self(ctx)
	if err != nil {
		report("self: %v", err)
		return
	}
	if err := p.Claim(self); err != nil {
		report("claim: %v", err)
		return
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if _, err := popup.WriteResult(opts.State, popup.Result{Action: "goto", SessionID: opts.Session, BlockID: strings.TrimSpace(line)}); err != nil {
		report("result: %v", err)
		return
	}
	err = p.Close(ctx)
	report("closed err=%v", err)
}
