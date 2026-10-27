-- rexkit's settings: name, default and what it does. Override any of them in
-- $XDG_CONFIG_HOME/rexkit/settings.lua (default ~/.config/rexkit/settings.lua),
-- a file that returns a table, e.g. `return { resize_step = 0.1 }`.
-- ⌃O c opens that file, creating it from this list when it is missing.
return {
  { "shell", "/opt/homebrew/bin/nu", "shell for new panes, the Scratch tab and Files" },
  { "editor", { "/opt/homebrew/bin/nvim" }, "editor command; the file is appended as one more argument" },
  { "resize_step", 0.05, "how far one resize key moves a border (fraction of the split)" },
  { "scratch_label", "Scratch", "name of the tab that holds parked panes" },
  { "zellij_modes", true, "Zellij-style modes: ⌃P pane, ⌃T tab, ⌃N resize, ⌃H move, ⌃S scroll, ⌃O session" },
  { "tmux_prefix", true, "tmux-style ⌃B prefix: one key, then back to normal" },
  { "alt_shortcuts", true, "global ⌥H ⌥J ⌥K ⌥L ⌥N ⌥I ⌥O ⌥= ⌥- shortcuts" },
  { "compact_labels", true, "short action titles in mode panels and the palette" },
  { "commit_via", "mode", "how ⏎ reaches an open tool: \"mode\" (a picker mode) or \"global\" (a global ⏎ binding)",
    choices = { "mode", "global" } },
  { "dismiss_on_blur", true, "close a tool when it loses focus" },
  { "backdrop", "clear", "around a tool's card: \"clear\" (the live panes, as they are) or \"dim\" (a dimmed copy of them, with a shadow under the card)",
    choices = { "dim", "clear" } },
  { "binary", "bin/rexkit", "the rexkit program; a relative path is resolved against rexkit's files (a clone, or Homebrew's opt/rexkit/libexec)" },
}
