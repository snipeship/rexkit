// Package popup runs a tool in a Rex floating layer: it finds the tool's own
// layer, closes it on every exit path (restoring the exact previous focus and
// the origin's zoom), and hands the tool's result to the Lua layer.
package popup

import "flag"

// Options are the flags the Lua layer passes to every floating tool.
type Options struct {
	Session       string // session the tool runs in
	OriginBlock   string // block that had focus when the tool opened
	OriginWindow  string // window of the origin block
	Zoomed        bool   // the origin block was zoomed (opening the float cancelled it)
	Client        string // client (app) that opened the tool
	State         string // private state directory of this tool instance
	DismissOnBlur bool   // close the tool when another pane takes focus
	Card          Card   // where in its float the tool draws (see Stage)
}

// RegisterFlags adds the shared flags to fs.
func (o *Options) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.Session, "session", "", "Rex session `ID` the tool runs in (default $REX_SESSION)")
	fs.StringVar(&o.OriginBlock, "origin-block", "", "block `ID` that had focus before the tool opened")
	fs.StringVar(&o.OriginWindow, "origin-window", "", "window `ID` of the origin block")
	fs.BoolVar(&o.Zoomed, "zoomed", false, "re-zoom the origin block when the tool closes")
	fs.StringVar(&o.Client, "client", "", "client `ID` that opened the tool")
	fs.StringVar(&o.State, "state", "", "private state `DIR` of this tool instance")
	fs.BoolVar(&o.DismissOnBlur, "dismiss-on-blur", true, "close the tool when another pane takes focus")
	fs.Var(cardArea{&o.Card}, "card", "draw in `x,y,w,h` (fractions of the window) of a whole-window float")
	fs.Var(cardCells{&o.Card}, "card-cells", "draw a fixed `COLSxROWS` centred in --card")
	fs.Var(backdropFlag{&o.Card}, "backdrop", "around the card: `dim` (the panes, dimmed) or clear (see-through)")
}
