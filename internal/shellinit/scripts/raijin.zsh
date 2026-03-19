typeset -h _RAIJIN_BIN="${RAIJIN_BIN:-raijin}"
typeset -h _RAIJIN_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-zsh-$$-$RANDOM}"
typeset -h _RAIJIN_BINDING_OWNER_PID="${RAIJIN_SESSION_BINDING_OWNER_PID:-$$}"

_raijin_exec() {
  RAIJIN_SESSION_BINDING_KEY="$_RAIJIN_BINDING_KEY" \
  RAIJIN_SESSION_BINDING_OWNER_PID="$_RAIJIN_BINDING_OWNER_PID" \
  "$_RAIJIN_BIN" "$@"
}

_raijin_main() {
  _raijin_exec "$@"
}

alias :='noglob _raijin_main'

# The saved original Tab widget (captured after all of .zshrc loads).
typeset -g _raijin_orig_tab_widget=".expand-or-complete"

_raijin_completion_widget() {
  # If we have zsh-autosuggestions, clear them before starting
  (( $+functions[_zsh_autosuggest_clear] )) && _zsh_autosuggest_clear

  # Only intercept lines that start with ":" (the raijin alias).
  # Everything else falls through to the original Tab widget (fzf, etc.).
  local trimmed="${LBUFFER##[[:space:]]}"
  if [[ -z "$trimmed" ]] || [[ "$trimmed" != :* ]]; then
    zle "$_raijin_orig_tab_widget"
    return
  fi

  # Invalidate the current line to allow interactive tools like FZF to
  # use the terminal cleanly.
  zle -I

  local original="$LBUFFER"
  local completed
  completed="$(_raijin_exec -complete "$LBUFFER" 2>/dev/null)"

  if [[ -z "$completed" || "$completed" == "$original" ]]; then
    zle "$_raijin_orig_tab_widget"
    return
  fi

  LBUFFER="$completed"

  # If there is nothing after the cursor, add a space to separate the
  # completed token from the next one.
  if [[ -z "$RBUFFER" ]]; then
    LBUFFER+=" "
  fi

  CURSOR=${#LBUFFER}
}

if [[ -o interactive ]] && (( $+builtins[zle] )); then
  # Defer binding until the first prompt, so all other plugins (fzf, etc.)
  # have already set up their Tab widgets. We capture the current ^I
  # binding as our fallback, then install raijin's widget on top.
  _raijin_deferred_bind() {
    # Read current ^I binding: output is like '"^I" widget-name'
    local cur
    cur="$(bindkey -M main '^I' 2>/dev/null)"
    cur="${cur#*\" }"  # strip everything up to and including '" '

    if [[ -n "$cur" && "$cur" != "raijin-completion-widget" ]]; then
      _raijin_orig_tab_widget="$cur"
    fi

    zle -N raijin-completion-widget _raijin_completion_widget
    bindkey -M main  '^I' raijin-completion-widget
    bindkey -M emacs '^I' raijin-completion-widget

    # Remove ourselves from precmd so we only run once.
    precmd_functions=("${(@)precmd_functions:#_raijin_deferred_bind}")
    unfunction _raijin_deferred_bind 2>/dev/null
  }

  precmd_functions+=(_raijin_deferred_bind)
fi
