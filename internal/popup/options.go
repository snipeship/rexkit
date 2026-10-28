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
	// TakeFocus: the float opened without focus, over a zoomed origin whose
	// screen the tool reads (Hints): focusing it ends the zoom, and the app
	// then rewraps the origin at its tiled size. The tool focuses its float
	// once it has read the origin (Popup.TakeFocus).
	TakeFocus bool
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
	fs.BoolVar(&o.TakeFocus, "take-focus", false, "the float opened without focus: focus it once the origin is read")
	// The Lua of a config not yet reloaded since rexkit was upgraded still
	// passes these (before Rex drew floats natively): accepted, and ignored.
	var retired string
	for _, name := range []string{"card", "card-cells", "backdrop"} {
		fs.StringVar(&retired, name, "", "no longer used")
	}
}
