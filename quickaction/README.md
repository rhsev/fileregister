# Finder Quick Actions

Two right-click actions for the Finder:

- **Add to binder** — a picker (last-used binder preselected, "New binder…"
  creates one), then `register add`, result as a notification. Idempotent —
  triggering it twice is harmless.
- **Which binder?** — `register of` for the selection, result as a dialog.

## Installation

```sh
make install-services
```

Copies the workflows to `~/Library/Services` and substitutes the repo path.
Keyboard shortcuts: System Settings → Keyboard → Keyboard Shortcuts → Services.

## Configuration

Automator starts without a shell environment, so the scripts read
`~/.config/fileregister/quickaction.env` instead:

```sh
GRUBBER_SET=notes
REGISTER_BIN=$HOME/bin/register   # default
```

Without that file they fall back to `GRUBBER_NOTES` / `GRUBBER_SET` from the
environment (in the Automator context: neither) and `~/bin/register`.

**Curated binder list:** if `~/.config/fileregister/binders` exists (one binder
per line, `#` comments), the picker shows only those — short and on-topic
instead of the full list. Keep the file current when you switch topics. Without
it, the picker lists every binder via `register list`.
