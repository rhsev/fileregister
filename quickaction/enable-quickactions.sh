#!/bin/zsh
# Enables installed .workflow services in the context menu. Automator writes this
# pbs entry when it saves a Quick Action; a manual cp to ~/Library/Services does
# not — the service is then registered but visible nowhere.
# Usage: enable-quickactions.sh <workflow-bundle>...

set -e

names=()
for wf in "$@"; do
  names+=("$(/usr/libexec/PlistBuddy -c 'Print :NSServices:0:NSMenuItem:default' "$wf/Contents/Info.plist")")
done
(( ${#names} > 0 )) || exit 0

tmp=$(mktemp)
defaults export pbs "$tmp"
python3 - "$tmp" "${names[@]}" <<'EOF'
import plistlib, sys
path = sys.argv[1]
with open(path, "rb") as f:
    d = plistlib.load(f)
st = d.setdefault("NSServicesStatus", {})
for name in sys.argv[2:]:
    st[f"(null) - {name} - runWorkflowAsService"] = {
        "enabled_context_menu": True,
        "enabled_services_menu": True,
    }
with open(path, "wb") as f:
    plistlib.dump(d, f)
EOF
defaults import pbs "$tmp"
rm -f "$tmp"

/System/Library/CoreServices/pbs -update
killall Finder 2>/dev/null || true
print "enabled in context menu: ${(j:, :)names}"
