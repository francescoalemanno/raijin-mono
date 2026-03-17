# Raijin shell integration for fish
# Add to your config.fish:  raijin --init fish | source
#
# Lines starting with ":" are forwarded to raijin with the leading ":"
# stripped.  Slash commands keep their slash:
#   :/models        →  raijin "/models"
#   :explain this   →  raijin "explain this"

# --- Key binding interception ---
# Rewrites the command line before execution when it starts with ":".
function _raijin_enter
    set -l buf (commandline)
    if string match -qr '^:' -- $buf
        set -l stripped (string replace -r '^:' '' -- $buf)
        commandline -r "raijin "(string escape -- $stripped)
    end
    commandline -f execute
end
bind \r _raijin_enter
bind \n _raijin_enter

# --- Tab completion via raijin engine ---
function __raijin_complete
    set -l line (commandline)
    if test -z "$line"
        set line (commandline -ct)
    end
    raijin --complete "$line" 2>/dev/null
end
complete -c : -f -a '(__raijin_complete)'
