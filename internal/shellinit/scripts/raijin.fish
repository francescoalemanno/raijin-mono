# Raijin shell integration for fish
# Add to your config.fish:  raijin --init fish | source
#
# This script autogenerates ":" shortcuts as aliases:
#   :               → raijin
#   :status         → raijin /status
#   :+skill         → raijin +skill

# --- Generated : aliases ---
alias : "raijin"
{{- range .CommandShortcuts }}
alias :{{ . }} "raijin /{{ . }}"
{{- end }}
{{- range .SkillShortcuts }}
alias :+{{ . }} "raijin +{{ . }}"
{{- end }}

# --- Completion for ":" alias ---
function __raijin_colon_complete
    raijin --complete (commandline) 2>/dev/null
end
complete -c : -f -a '(__raijin_colon_complete)'
