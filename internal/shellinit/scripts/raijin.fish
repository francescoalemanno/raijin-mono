# Raijin shell integration for fish
# Add to your config.fish:  raijin --init fish | source
#
# This provides the ":" alias for raijin

# Export binding context so direct raijin invocations also work
if test -z "$RAIJIN_SESSION_BINDING_KEY"
    set -gx RAIJIN_SESSION_BINDING_KEY "shell-fish-$fish_pid-(random)"
end
if test -z "$RAIJIN_SESSION_BINDING_OWNER_PID"
    set -gx RAIJIN_SESSION_BINDING_OWNER_PID "$fish_pid"
end

function __raijin_main
    "{{.RaijinBin}}" $argv
end

# --- Main : alias ---
alias : "__raijin_main"

# --- Completion for ":" alias ---
function __raijin_colon_complete
    "{{.RaijinBin}}" -complete-list (commandline) 2>/dev/null
end
complete -c : -f -a "(__raijin_colon_complete)"
