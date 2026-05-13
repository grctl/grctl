# grctl demo tape
# Requires: vhs (github.com/charmbracelet/vhs)
# Usage: vhs demo.tape

Output demo.gif

Require grctl

Set Shell "bash"
Set FontSize 32
Set Width 3024
Set Height 1964
Set Padding 0

# Show version
# Type "grctl version"
# Enter
# Sleep 1s
# Ctrl+L

# Show help
# Type "grctl --help"
# Enter
# Sleep 1.5s
# Ctrl+L

# Show workflow subcommands
# Type "grctl workflow --help"
# Enter
# Sleep 1.5s
# Ctrl+L

# Start a Hello workflow
Type "grctl workflow start --type Hello --input-file sdk-python/examples/hello_input.json"
Enter
Sleep 3s
Ctrl+L

# Start a Payment workflow with a custom ID
# Type "grctl workflow start --type Payment --id payment-ORDER-001 --input-file sdk-python/examples/payment_input.json"
# Enter
# Sleep 3s
# Ctrl+L

# Open the workflow browser (TUI)
Type "grctl workflow list"
Enter
Sleep 5s

# Navigate down a few rows
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 300ms

# Press Enter to open workflow history
Enter
Sleep 5s
Enter

# Navigate through history events
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s

# Go back
Escape
Sleep 1s

# Quit the TUI
Type "q"
Sleep 500ms
