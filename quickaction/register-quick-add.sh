#!/bin/zsh
# register-quick-add — Finder Quick Action: add the selected files to a binder.
# The binder is picked in a dialog (last-used binder preselected, "New binder…"
# creates one); the result comes back as a notification.
#
# Automator starts WITHOUT a login shell environment: GRUBBER_SET/GRUBBER_NOTES
# and the path to the register binary come from
# ~/.config/fileregister/quickaction.env (or the defaults below), not from your
# shell config.
#
# Headless mode (for tests/scripting): REGISTER_QA_BINDER=<name> skips every
# dialog and writes the result to stdout instead of posting a notification.

set -u

REGISTER_BIN="${REGISTER_BIN:-$HOME/bin/register}"
ENV_FILE="$HOME/.config/fileregister/quickaction.env"
[[ -r "$ENV_FILE" ]] && source "$ENV_FILE"
[[ -n "${GRUBBER_SET:-}" ]]    && export GRUBBER_SET
[[ -n "${GRUBBER_NOTES:-}" ]]  && export GRUBBER_NOTES
[[ -n "${GRUBBER_CONFIG:-}" ]] && export GRUBBER_CONFIG

# Automator's PATH is minimal, and register needs its fileanchor engine: keep
# ~/bin (where `make link` puts both) and the usual prefixes on PATH. Without
# ~/bin, `of` found no engine and answered with an error.
export PATH="$HOME/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

NEW_LABEL="New binder…"
CACHE_DIR="$HOME/.cache/fileregister"
LAST_FILE="$CACHE_DIR/last-binder"

headless() { [[ -n "${REGISTER_QA_BINDER:-}" ]] }

die() {
  if headless; then
    print -r -- "Error: $1" >&2
  else
    osascript -e 'on run argv' \
              -e 'display dialog (item 1 of argv) with title "fileregister" buttons {"OK"} default button 1 with icon caution' \
              -e 'end run' -- "$1" >/dev/null 2>&1
  fi
  exit 1
}

(( $# > 0 )) || die "No files given."
[[ -x "$REGISTER_BIN" ]] || die "register not found: $REGISTER_BIN — set REGISTER_BIN in $ENV_FILE."

# --- pick a binder -----------------------------------------------------------
if headless; then
  binder="$REGISTER_QA_BINDER"
else
  # Curated list: if ~/.config/fileregister/binders exists (one binder per line,
  # # comments), the dialog shows ONLY those — short and on-topic; keep the file
  # current when you switch topics. Without it: every binder via `register list`.
  FAV_FILE="$HOME/.config/fileregister/binders"
  binders=()
  if [[ -r "$FAV_FILE" ]]; then
    binders=("${(@f)$(grep -v '^[[:space:]]*#' "$FAV_FILE" | grep -v '^[[:space:]]*$')}")
    binders=(${binders:#})
  fi

  if (( ${#binders} == 0 )); then
    # Binder list = `register list` minus the "(N files)" column. An error
    # (typically GRUBBER_SET/GRUBBER_NOTES misconfigured) has to surface here —
    # otherwise the dialog just looks empty and the add fails later.
    list_out=$("$REGISTER_BIN" list 2>&1)
    if (( $? != 0 )); then
      die "Check the configuration ($ENV_FILE): $list_out"
    fi
    binders=("${(@f)$(print -r -- "$list_out" \
      | sed -E 's/[[:space:]]+\([0-9]+ files?\)$//' \
      | grep -v '^No active binders found')}")
    binders=(${binders:#})
  fi

  last="$NEW_LABEL"
  [[ -r "$LAST_FILE" ]] && last="$(<"$LAST_FILE")"

  binder=$(osascript - "$last" "$NEW_LABEL" "${binders[@]}" <<'EOS'
on run argv
  set defaultName to item 1 of argv
  set theList to rest of argv
  set sel to choose from list theList with title "fileregister" with prompt "Add to which binder?" default items {defaultName}
  if sel is false then return ""
  return item 1 of sel
end run
EOS
  )
  [[ -z "$binder" ]] && exit 0   # cancelled

  if [[ "$binder" == "$NEW_LABEL" ]]; then
    binder=$(osascript <<'EOS'
try
  set d to display dialog "Name of the new binder:" with title "fileregister" default answer ""
  return text returned of d
on error
  return ""
end try
EOS
    )
    [[ -z "${binder// /}" ]] && exit 0
  fi
fi

# --- run the add -------------------------------------------------------------
out=$("$REGISTER_BIN" add "$@" --binder "$binder" 2>&1)
(( $? == 0 )) || die "register add failed: $out"

mkdir -p "$CACHE_DIR" && print -r -- "$binder" > "$LAST_FILE"

appended=$(print -r -- "$out" | sed -n -E 's/^ *appended *: *([0-9]+).*/\1/p')
noop=$(print -r -- "$out"     | sed -n -E 's/^ *noop *: *([0-9]+).*/\1/p')
msg="${appended:-0} added"
[[ -n "$noop" && "$noop" != "0" ]] && msg="$msg, $noop already there"

if headless; then
  print -r -- "$msg → $binder"
else
  osascript -e 'on run argv' \
            -e 'display notification (item 1 of argv) with title "fileregister"' \
            -e 'end run' -- "$msg → $binder" >/dev/null 2>&1
fi
