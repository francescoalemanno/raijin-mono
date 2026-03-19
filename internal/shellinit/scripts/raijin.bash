# Raijin shell integration for bash
# Add to your .bashrc:  eval "$(raijin --init bash)"
#
# This provides the ":" alias for raijin

# Export binding context so direct raijin invocations also work
export RAIJIN_SESSION_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-bash-$$-$RANDOM}"
export RAIJIN_SESSION_BINDING_OWNER_PID="${RAIJIN_SESSION_BINDING_OWNER_PID:-$$}"
_raijin_main() {
  "{{.RaijinBin}}" "$@"
}

# --- Main : alias ---
alias :='_raijin_main'

# --- Completion for ":" alias ---
_raijin_colon_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local line="${COMP_LINE}"
  
  # Don't complete the command name itself (word index 0)
  if [[ $COMP_CWORD -eq 0 ]]; then
    COMPREPLY=()
    return
  fi
  
  # For empty command line, construct the line from words
  [[ -n "$line" ]] || line=": ${COMP_WORDS[*]:1}"
  
  local out
  out="$("{{.RaijinBin}}" -complete-list "$line" 2>/dev/null)"
  COMPREPLY=()
  while IFS= read -r item; do
    [[ -n "$item" ]] || continue
    COMPREPLY+=( "$item" )
  done <<< "$out"
  
  # Disable space suffix so completion doesn't add unwanted space
  compopt -o nospace 2>/dev/null || true
}
complete -F _raijin_colon_complete :
