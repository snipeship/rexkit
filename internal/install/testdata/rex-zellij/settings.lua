return {
  -- Rex's sequence-hint preference has not hidden persistent mode panels.
  pane_key_sequences = false, -- Keep navigation active until Escape.
  tiny_navigation = false, -- Preserve full pane-mode commands after navigation.
  compact_labels = true,
  compact_menus = true, -- Inherit global shortcuts instead of listing them in every mode.
  shell = "/opt/homebrew/bin/nu",
  python = "/opt/homebrew/bin/python3",
  editor = { "/opt/homebrew/bin/nvim" },
  resize_step = 0.05,
  scratch_label = "Scratch",
  alt_shortcuts = true,
}
