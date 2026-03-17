# Raijin shell integration for bash
# Add to your .bashrc:  eval "$(raijin --init bash)"
#
# Lines starting with ":" are forwarded to raijin with the leading ":"
# stripped.  Slash commands keep their slash:
#   :/models        →  raijin "/models"
#   :explain this   →  raijin "explain this"

# --- Readline interception ---
# Rewrites the line buffer before Enter submits it, so tokens like ":/tree"
# are never interpreted as file paths by bash.
_raijin_intercept() {
  if [[ "$READLINE_LINE" == :* ]]; then
    local stripped="${READLINE_LINE#:}"
    READLINE_LINE="raijin $(printf '%q' "$stripped")"
    READLINE_POINT=${#READLINE_LINE}
  fi
}
# Bind to an internal key sequence, then chain Enter through it.
bind -x '"\C-x\C-r": _raijin_intercept'
bind '"\C-m": "\C-x\C-r\C-m"'
bind '"\C-j": "\C-x\C-r\C-j"'

# --- Tab completion via raijin engine ---
_raijin_colon_complete() {
  local first="${COMP_WORDS[0]}"
  local cur="${COMP_WORDS[COMP_CWORD]}"
  [[ "$cur" == :* || "$first" == :* ]] || return 1

  local line="$COMP_LINE"
  [[ -n "$line" ]] || line="$cur"

  local completions
  completions="$(raijin --complete "$line" 2>/dev/null)"

  local -a matches=()
  while IFS= read -r line; do
    if [[ -n "$line" ]]; then
      matches+=( "$line" )
    fi
  done <<< "$completions"

  COMPREPLY=( "${matches[@]}" )
  return 0
}

complete -D -F _raijin_colon_complete
