---
name: tmux-test
description: Interacts with any CLI/TUI application via TMUX.
---

<purpose>
Guide users through testing any CLI or TUI application using tmux, enabling programmatic control and live capture of the terminal interface while it's running.
</purpose>

<instructions>
Use tmux to test and interact with any CLI/TUI application with the following workflow:

## Setup

1. **Start a detached tmux session:**
   ```bash
   tmux new-session -d -s <session_name> -x 135 -y 40 '<command_to_run>'
   ```

   **Always use terminal size 135x40** (`-x 135 -y 40`) for consistent testing.

   Examples:
   - `tmux new-session -d -s test_app -x 135 -y 40 './myapp'`
   - `tmux new-session -d -s test_node -x 135 -y 40 'node server.js'`
   - `tmux new-session -d -s test_python -x 135 -y 40 'python script.py'`

2. **Verify session is running:**
   ```bash
   tmux list-sessions
   ```

## Capturing the Live View

**Capture and print the current pane output:**
```bash
tmux capture-pane -t <session_name> -p
```

This shows the exact application state at that moment, including prompts, menus, status indicators, and any output.

## Sending Commands

**Send keys/commands to the application:**
```bash
tmux send-keys -t <session_name> "<command>" Enter
```

Examples vary by application:
- CLI tools: `ls -la`, `git status`, `npm test`
- Interactive prompts: `yes`, `no`, username/password inputs
- TUI apps: Menu commands like `:q`, `/search`, `F1`

## Navigating TUI Elements

**For dialogs, menus, and interactive prompts:**
```bash
tmux send-keys -t <session_name> <key>
```

Common navigation keys:
- `Up`/`Down` - Navigate list items, menu options
- `Enter` - Select/confirm, submit input
- `Escape` - Cancel/close dialog, exit mode
- `Ctrl+c` - Cancel operation, interrupt
- `Tab` - Navigate between fields, move focus
- `Space` - Toggle selections, advance pages

Multiple keys can be chained: `tmux send-keys -t <session_name> Down Down Enter`

**Send special keys:**
```bash
tmux send-keys -t <session_name> C-c  # Ctrl+c
tmux send-keys -t <session_name> C-d  # Ctrl+d
tmux send-keys -t <session_name> C-l  # Ctrl+l (clear screen)
```

## Testing Workflows

**Example: Testing a CLI tool:**
```bash
# Start session
tmux new-session -d -s cli_test './mycli'
sleep 1
tmux capture-pane -t cli_test -p  # View initial prompt

# Send command and capture response
tmux send-keys -t cli_test "status" Enter
sleep 2
tmux capture-pane -t cli_test -p  # Verify output

# Navigate menu if present
tmux send-keys -t cli_test Down Enter
sleep 1
tmux capture-pane -t cli_test -p  # Confirm selection
```

**Example: Testing a TUI application:**
```bash
# Start session
tmux new-session -d -s tui_test './mytuiapp'
sleep 2

# Navigate menus
tmux send-keys -t tui_test F1  # Open help menu
sleep 1
tmux capture-pane -t tui_test -p  # View help dialog
tmux send-keys -t tui_test Escape  # Close help

# Interact with form
tmux send-keys -t tui_test Tab  # Move to next field
tmux send-keys -t tui_test "input value" Enter
sleep 1
tmux capture-pane -t tui_test -p  # Verify form state
```

**Example: Testing an interactive REPL:**
```bash
# Start Python REPL
tmux new-session -d -s python_test 'python3'
sleep 1

# Send commands
tmux send-keys -t python_test "print('Hello')" Enter
sleep 1
tmux capture-pane -t python_test -p  # Should show "Hello"

# Test multiline input
tmux send-keys -t python_test "def test():" Enter
tmux send-keys -t python_test "    return 42" Enter
tmux send-keys -t python_test Enter
sleep 1
tmux capture-pane -t python_test -p  # Verify function definition

# Exit REPL
tmux send-keys -t python_test "exit()" Enter
```

## Cleanup

**Kill the tmux session:**
```bash
tmux kill-session -t <session_name>
```

**Verify cleanup:**
```bash
tmux list-sessions  # Should show no sessions
```

## Key TMUX Commands Summary

| Command | Purpose |
|---------|---------|
| `tmux new-session -d -s NAME '<command>'` | Start detached session with application |
| `tmux list-sessions` | List all sessions |
| `tmux capture-pane -t NAME -p` | Capture and print pane |
| `tmux send-keys -t NAME "text" Enter` | Send keys to pane |
| `tmux send-keys -t NAME C-c` | Send Ctrl+c (interrupt) |
| `tmux kill-session -t NAME` | Kill session |

## Advanced TMUX Features

**Capture to file:**
```bash
tmux capture-pane -t <session_name> -p > output.txt
```

**Capture with line range:**
```bash
tmux capture-pane -t <session_name> -S -100 -p  # Last 100 lines
```

**Wait for pattern in output:**
```bash
# Capture repeatedly until pattern appears
while ! tmux capture-pane -t test -p | grep -q "Ready"; do
  sleep 0.5
done
```

**Send files as input:**
```bash
tmux send-keys -t <session_name> C-l  # Clear screen
cat commands.txt | while read -r line; do
  tmux send-keys -t <session_name> "$line" Enter
  sleep 1
done
```

## Pro Tips

1. **Use sleep strategically** - Add delays after sending keys to allow the application to process input and update its UI
2. **Capture after each action** - Use `capture-pane` to verify the application state after each interaction
3. **Watch for application-specific indicators** - Look for status messages, prompts, or success/failure indicators
4. **Test interactive flows** - Chain multiple commands to test complete workflows (login → navigate → action → logout)
5. **Verify outputs** - Check captured output for expected results, error messages, or confirmation prompts

## Common Testing Patterns

- **Smoke tests** → Start app, capture initial state, verify it's running
- **Command execution** → Send command, wait for response, capture and verify output
- **UI navigation** → Send navigation keys, capture to verify menu/dialog state
- **Form input** → Send input values, capture to verify form state
- **Error handling** → Trigger error conditions, capture to verify error messages
- **Multi-step workflows** → Chain multiple interactions to test complete user journeys

## Debugging

**If capture shows nothing:**
- Session may have exited: `tmux list-sessions`
- Application may be waiting for input: Check for prompts
- Output may be outside viewport: Use `-S` flag to capture more lines

**If keys aren't being sent:**
- Verify session name: `tmux list-sessions`
- Check for special characters: Some apps require specific key sequences
- Application may have crashed: Check captured output for errors

**If timing is off:**
- Increase sleep values for slower applications
- Use loops to wait for specific output patterns
- Test manually first to understand application responsiveness
</instructions>
