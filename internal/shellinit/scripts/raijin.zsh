# Raijin shell integration for zsh
# Add to your .zshrc:  eval "$(raijin --init zsh)"
#
# This script autogenerates ":" shortcuts as aliases:
#   :               → raijin
#   :status         → raijin /status
#   :+skill         → raijin +skill

typeset -h _RAIJIN_BIN="${RAIJIN_BIN:-raijin}"
typeset -h _RAIJIN_BINDING_KEY="${RAIJIN_SESSION_BINDING_KEY:-shell-zsh-$$-$RANDOM}"
typeset -h _RAIJIN_BINDING_OWNER_PID="${RAIJIN_SESSION_BINDING_OWNER_PID:-$$}"

# --- Generated : aliases ---
# Use noglob to prevent zsh glob expansion on special characters like ?, *, etc.
# The wrapper function receives arguments unexpanded.
_raijin_main() {
  RAIJIN_SESSION_BINDING_KEY="$_RAIJIN_BINDING_KEY" \
  RAIJIN_SESSION_BINDING_OWNER_PID="$_RAIJIN_BINDING_OWNER_PID" \
  "$_RAIJIN_BIN" "$@"
}
alias :='noglob _raijin_main'
{{- range .CommandShortcuts }}
alias :{{ . }}='noglob _raijin_main /{{ . }}'
{{- end }}
{{- range .SkillShortcuts }}
alias :+{{ . }}='noglob _raijin_main +{{ . }}'
{{- end }}

_raijin_complete_candidates() {
  local line="$1"
  [[ -n "$line" ]] || line=":"
  local out
  out="$("$_RAIJIN_BIN" --complete "$line" 2>/dev/null)"
  while IFS= read -r item; do
    [[ -n "$item" ]] || continue
    # Raijin commands are exposed as :command aliases in zsh, not :/command.
    if [[ "$item" == :/* ]]; then
      item=":${item#:/}"
    fi
    printf '%s\n' "$item"
  done <<< "$out"
}

_raijin_format_mention() {
  local path="$1"
  if [[ "$path" == *[[:space:]]* ]]; then
    local escaped="$path"
    escaped="${escaped//\\/\\\\}"
    escaped="${escaped//\"/\\\"}"
    printf '@"%s"' "$escaped"
    return
  fi
  printf '@%s' "$path"
}

_raijin_should_use_command_picker() {
  local line="$1"
  local current_word="$2"
  [[ "$current_word" == :* || "$current_word" == +* || "$current_word" == /* ]] && return 0
  [[ "$line" =~ '^:([[:space:]]*)?$' ]] && return 0
  [[ "$line" =~ '^:[^[:space:]]*$' ]] && return 0
  return 1
}

_raijin_completion_widget() {
  local line="$LBUFFER"
  local current_word="${LBUFFER##*[[:space:]]}"
  local left_len=$(( ${#LBUFFER} - ${#current_word} ))
  local left_buffer="${LBUFFER[1,$left_len]}"

  # Forge-style @ completion using Raijin's embedded fzf-backed file picker.
  if [[ "$current_word" == @* ]]; then
    local query="${current_word#@}"
    local selected
    selected="$("$_RAIJIN_BIN" --fzf paths --fzf-query "$query" 2>/dev/null)"

    if [[ -n "$selected" ]]; then
      local mention
      mention="$(_raijin_format_mention "$selected")"
      LBUFFER="${left_buffer}${mention}"
      CURSOR=${#LBUFFER}
    fi
    zle reset-prompt
    return 0
  fi

  # Forge-style completion for :command and +skill tokens, driven by raijin --complete.
  if _raijin_should_use_command_picker "$line" "$current_word"; then
    local -a completions
    completions=("${(@f)$(_raijin_complete_candidates "$line")}")
    (( ${#completions[@]} > 0 )) || { zle redisplay; return 0; }

    local selected=""
    if (( ${#completions[@]} == 1 )); then
      selected="${completions[1]}"
    else
      selected="$(printf '%s\n' "${completions[@]}" | "$_RAIJIN_BIN" --fzf complete --fzf-query "$current_word" 2>/dev/null)"
    fi

    if [[ -n "$selected" ]]; then
      LBUFFER="${left_buffer}${selected}"
      CURSOR=${#LBUFFER}
      if [[ -z "$RBUFFER" ]]; then
        LBUFFER+=" "
        CURSOR=${#LBUFFER}
      fi
    fi
    zle reset-prompt
    return 0
  fi

  zle .expand-or-complete
}

_raijin_bracketed_paste() {
  zle .$WIDGET "$@"
  zle redisplay
  zle reset-prompt
}

_raijin_accept_line() {
  if [[ "$BUFFER" =~ '^:/([a-zA-Z0-9_.+-]+)([[:space:]].*)?$' ]]; then
    BUFFER=":${match[1]}${match[2]}"
  fi
  zle accept-line
}

_raijin_bind_widget_keys() {
  bindkey -M main '^I' raijin-completion-widget
  bindkey -M emacs '^I' raijin-completion-widget
  bindkey -M viins '^I' raijin-completion-widget
  bindkey -M main '^M' raijin-accept-line
  bindkey -M main '^J' raijin-accept-line
  bindkey -M emacs '^M' raijin-accept-line
  bindkey -M emacs '^J' raijin-accept-line
  bindkey -M viins '^M' raijin-accept-line
  bindkey -M viins '^J' raijin-accept-line
  bindkey -M vicmd '^M' raijin-accept-line
  bindkey -M vicmd '^J' raijin-accept-line
}

_raijin_rebind_widgets_hook() {
  _raijin_bind_widget_keys
}

_raijin_enable_syntax_highlighting() {
  [[ -n "${ZSH_HIGHLIGHT_HIGHLIGHTERS-}" ]] || return 0
  if [[ -z "${_RAIJIN_HIGHLIGHT_ENABLED-}" ]]; then
    ZSH_HIGHLIGHT_PATTERNS+=('@[^[:space:]@]+|@"([^"\\]|\\.)+"|@'"'"'([^'"'"'\\]|\\.)+'"'"'' 'fg=cyan,bold')
    ZSH_HIGHLIGHT_PATTERNS+=('(#s):[a-zA-Z0-9_./+-]#' 'fg=yellow,bold')
    if [[ " ${ZSH_HIGHLIGHT_HIGHLIGHTERS[*]} " != *" pattern "* ]]; then
      ZSH_HIGHLIGHT_HIGHLIGHTERS+=(pattern)
    fi
    typeset -g _RAIJIN_HIGHLIGHT_ENABLED=1
  fi
}

_raijin_enable_syntax_highlighting_precmd() {
  _raijin_enable_syntax_highlighting
  [[ -n "${_RAIJIN_HIGHLIGHT_ENABLED-}" ]] || return 0
  precmd_functions=(${precmd_functions:#_raijin_enable_syntax_highlighting_precmd})
}

_raijin_register_widgets() {
  [[ -o interactive ]] || return 1
  (( $+builtins[zle] )) || return 1
  zle -N raijin-completion-widget _raijin_completion_widget
  zle -N raijin-accept-line _raijin_accept_line
  _raijin_bind_widget_keys
  zle -N bracketed-paste _raijin_bracketed_paste
  return 0
}
_raijin_register_widgets_precmd() {
  _raijin_register_widgets || return 0
}
if ! _raijin_register_widgets; then
  if [[ " ${precmd_functions[*]} " != *" _raijin_register_widgets_precmd "* ]]; then
    precmd_functions+=(_raijin_register_widgets_precmd)
  fi
fi

_raijin_register_widget_hooks() {
  [[ -o interactive ]] || return 1
  (( $+builtins[zle] )) || return 1
  autoload -Uz add-zle-hook-widget 2>/dev/null || return 1
  if [[ -z "${_RAIJIN_WIDGET_HOOKS_REGISTERED-}" ]]; then
    add-zle-hook-widget line-init _raijin_rebind_widgets_hook >/dev/null 2>&1 || return 1
    add-zle-hook-widget keymap-select _raijin_rebind_widgets_hook >/dev/null 2>&1 || return 1
    typeset -g _RAIJIN_WIDGET_HOOKS_REGISTERED=1
  fi
  return 0
}
_raijin_register_widget_hooks_precmd() {
  _raijin_register_widget_hooks || return 0
  precmd_functions=(${precmd_functions:#_raijin_register_widget_hooks_precmd})
}
if ! _raijin_register_widget_hooks; then
  if [[ " ${precmd_functions[*]} " != *" _raijin_register_widget_hooks_precmd "* ]]; then
    precmd_functions+=(_raijin_register_widget_hooks_precmd)
  fi
fi

_raijin_enable_syntax_highlighting
if [[ -z "${_RAIJIN_HIGHLIGHT_ENABLED-}" ]]; then
  if [[ " ${precmd_functions[*]} " != *" _raijin_enable_syntax_highlighting_precmd "* ]]; then
    precmd_functions+=(_raijin_enable_syntax_highlighting_precmd)
  fi
fi

# --- Completion for ":" alias ---
_raijin_colon_complete() {
  local line="${(j: :)words}"
  local -a completions
  completions=("${(@f)$(_raijin_complete_candidates "$line")}")
  (( ${#completions[@]} > 0 )) || return 1
  compadd -Q -S " " -- "${completions[@]}"
  return 0
}
_raijin_register_colon_completion() {
  (( $+functions[compdef] )) || return 1
  compdef _raijin_colon_complete : raijin
  return 0
}
_raijin_register_colon_completion_precmd() {
  _raijin_register_colon_completion || return 0
  precmd_functions=(${precmd_functions:#_raijin_register_colon_completion_precmd})
}
if ! _raijin_register_colon_completion; then
  if [[ " ${precmd_functions[*]} " != *" _raijin_register_colon_completion_precmd "* ]]; then
    precmd_functions+=(_raijin_register_colon_completion_precmd)
  fi
fi

_raijin_colon_completer() {
  [[ "${words[1]}" == ":" ]] || return 1
  local line="${BUFFER}"
  [[ -n "$line" ]] || line="${(j: :)words}"
  local -a completions
  completions=("${(@f)$(_raijin_complete_candidates "$line")}")
  (( ${#completions[@]} > 0 )) || return 1
  compadd -Q -S " " -- "${completions[@]}"
  return 0
}
if [[ -o interactive ]] && (( $+builtins[zle] )); then
  typeset -ga _raijin_existing_completers
  if zstyle -a ':completion:*' completer _raijin_existing_completers; then
    typeset -gi _raijin_has_colon_completer=0
    for _raijin_c in "${_raijin_existing_completers[@]}"; do
      if [[ "$_raijin_c" == "_raijin_colon_completer" ]]; then
        _raijin_has_colon_completer=1
        break
      fi
    done
    if (( !_raijin_has_colon_completer )); then
      zstyle ':completion:*' completer _raijin_colon_completer "${_raijin_existing_completers[@]}"
    fi
  else
    zstyle ':completion:*' completer _raijin_colon_completer _complete _ignored
  fi
fi
