# Raijin shell integration for bash
# Add to your .bashrc:  eval "$(raijin --init bash)"
#
# This provides the ":" alias for raijin

_RAIJIN_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-bash-$$-$RANDOM}"
_RAIJIN_BINDING_OWNER_PID="${RAIJIN_SESSION_BINDING_OWNER_PID:-$$}"
_raijin_main() {
  RAIJIN_SESSION_BINDING_KEY="$_RAIJIN_BINDING_KEY" \
  RAIJIN_SESSION_BINDING_OWNER_PID="$_RAIJIN_BINDING_OWNER_PID" \
  command raijin "$@"
}

# --- Main : alias ---
alias :='_raijin_main'

# --- Completion for ":" alias ---
_raijin_colon_complete() {
  local line="${COMP_LINE}"
  [[ -n "$line" ]] || line=": ${COMP_WORDS[*]:1}"
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local out
  out="$(raijin -complete "$line" 2>/dev/null)"
  COMPREPLY=()
  while IFS= read -r item; do
    [[ -n "$item" ]] || continue
    if [[ "$cur" != :* && "$item" == :* ]]; then
      item="${item#:}"
    fi
    COMPREPLY+=( "$item" )
  done <<< "$out"
}
complete -F _raijin_colon_complete :
