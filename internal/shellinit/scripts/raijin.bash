# Raijin shell integration for bash
# Add to your .bashrc:  eval "$(raijin --init bash)"
#
# This script autogenerates ":" shortcuts as aliases:
#   :               → raijin
#   :status         → raijin /status
#   :+skill         → raijin +skill

# --- Generated : aliases ---
alias :='raijin'
{{- range .CommandShortcuts }}
alias :{{ . }}='raijin /{{ . }}'
{{- end }}
{{- range .SkillShortcuts }}
alias :+{{ . }}='raijin +{{ . }}'
{{- end }}

# --- Completion for ":" alias ---
_raijin_colon_complete() {
  local line="${COMP_LINE}"
  [[ -n "$line" ]] || line=": ${COMP_WORDS[*]:1}"
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local out
  out="$(raijin --complete "$line" 2>/dev/null)"
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
