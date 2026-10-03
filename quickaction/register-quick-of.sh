#!/bin/zsh
# register-quick-of — Finder Quick Action: shows in a dialog which binders the
# selected files belong to (reverse lookup, `register of`).
#
# Environment resolution as in register-quick-add.sh (Automator has no shell
# config). Headless mode: REGISTER_QA_HEADLESS=1 writes to stdout, no dialog.

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

(( $# > 0 )) || exit 0
[[ -x "$REGISTER_BIN" ]] || exit 1

report=""
for f in "$@"; do
  out=$("$REGISTER_BIN" of "$f" 2>&1)
  report+="${f:t}"$'\n'"$out"$'\n\n'
done

if [[ -n "${REGISTER_QA_HEADLESS:-}" ]]; then
  print -r -- "$report"
else
  osascript -e 'on run argv' \
            -e 'display dialog (item 1 of argv) with title "fileregister" buttons {"OK"} default button 1' \
            -e 'end run' -- "$report" >/dev/null 2>&1
fi
