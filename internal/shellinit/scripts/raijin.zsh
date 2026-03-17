# Raijin shell integration for zsh
# Add to your .zshrc:  eval "$(raijin --init zsh)"
#
# Lines starting with ":" are forwarded to raijin with the leading ":"
# stripped.  Slash commands keep their slash:
#   :/models        →  raijin "/models"
#   :explain this   →  raijin "explain this"

# --- ZLE accept-line override ---
# Intercepts Enter at the line-editor level (before parsing), so tokens
# like ":/tree" are never interpreted as file paths.
_raijin_accept_line() {
  if [[ "$BUFFER" == :* ]]; then
    local stripped="${BUFFER#:}"
    BUFFER="raijin ${(qq)stripped}"
  fi
  zle .accept-line
}
zle -N accept-line _raijin_accept_line

# --- Tab completion via raijin engine ---
_raijin_colon() {
  local first="${words[1]}"
  local cur="${words[CURRENT]}"
  [[ "$cur" == :* || "$first" == :* ]] || return 1

  local line="${BUFFER}"
  [[ -n "$line" ]] || line="$cur"

  local -a completions
  completions=("${(@f)$(raijin --complete "$line" 2>/dev/null)}")
  compadd -S " " -- "${completions[@]}"
  return 0
}

# Insert our completer before the default ones.
zstyle ':completion:*' completer _raijin_colon _complete _ignored
