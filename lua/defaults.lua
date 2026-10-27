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
  { "alt_shortcuts", true, "global ⌥H ⌥J ⌥K ⌥L ⌥N ⌥I ⌥O ⌥= ⌥- ⌥A ⌥⇧A shortcuts" },
  { "compact_labels", true, "short action titles in mode panels and the palette" },
  { "commit_via", "mode", "how ⏎ reaches an open tool: \"mode\" (a picker mode) or \"global\" (a global ⏎ binding)",
    choices = { "mode", "global" } },
  { "dismiss_on_blur", true, "close a tool when it loses focus" },
  { "backdrop", "clear", "around a tool's card: \"clear\" (the live panes, as they are) or \"dim\" (a dimmed copy of them, with a shadow under the card)",
    choices = { "dim", "clear" } },
  { "agent_toasts", true, "toast when an agent in another pane needs you" },
  { "agent_toast_states", { "blocked", "failed", "done" }, "which agents toast: blocked (a question, a permission), failed, done; {} for none",
    items = { "blocked", "failed", "done" } },
  { "agent_key", "ctrl+shift+a", "a global key that goes to the next agent that needs you; \"\" for none (⌥A and ⌃O a do too, when those keys are on)" },
  { "agent_back_key", "ctrl+alt+shift+a", "a global key that goes back to where the next-agent presses began; \"\" for none (⌥⇧A and ⌃O ⇧A do too, when those keys are on)" },
  { "hints_key", "", "a global key that opens Hints, such as \"ctrl+shift+f\"; \"\" for none (⌃S f and ⌃B ⇧F do, when those modes are on)" },
  { "binary", "bin/rexkit", "the rexkit program; a relative path is resolved against rexkit's files (a clone, or Homebrew's opt/rexkit/libexec)" },
}
