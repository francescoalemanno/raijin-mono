# Raijin shell integration for fish
# Add to your config.fish:  raijin --init fish | source
#
# This provides the ":" alias for raijin

set -g __raijin_binding_key "$RAIJIN_SESSION_BINDING_KEY"
if test -z "$__raijin_binding_key"
    set -g __raijin_binding_key "shell-fish-$fish_pid-(random)"
end
set -g __raijin_binding_owner_pid "$RAIJIN_SESSION_BINDING_OWNER_PID"
if test -z "$__raijin_binding_owner_pid"
    set -g __raijin_binding_owner_pid "$fish_pid"
end

function __raijin_main
    set -lx RAIJIN_SESSION_BINDING_KEY "$__raijin_binding_key"
    set -lx RAIJIN_SESSION_BINDING_OWNER_PID "$__raijin_binding_owner_pid"
    command raijin $argv
end

# --- Main : alias ---
alias : "__raijin_main"

# --- Completion for ":" alias ---
function __raijin_colon_complete
    raijin -complete (commandline) 2>/dev/null
end
complete -c : -f -a '(__raijin_colon_complete)'
