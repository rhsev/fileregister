# Finder Quick Actions

Two right-click actions for the Finder:

- **Add to binder** opens a picker (last-used binder preselected, "New binder…"
  creates one), runs `register add` and reports the result as a notification.
  It is idempotent, so triggering it twice is harmless.
- **Which binder?** runs `register of` on the selection and shows the result in
  a dialog.

## Installation

```sh
make install-services
```

Copies the workflows to `~/Library/Services`, substitutes the repo path and
turns both actions on in the Finder's context menu. Keyboard shortcuts are set in
System Settings → Keyboard → Keyboard Shortcuts → Services.

## Configuration

Automator starts without a shell environment, so the scripts read
`~/.config/fileregister/quickaction.env` instead:

```sh
GRUBBER_SET=notes
REGISTER_BIN=$HOME/bin/register   # default
```

The file can also set `GRUBBER_NOTES` and `GRUBBER_CONFIG`. Without it, the
scripts fall back to `GRUBBER_NOTES` and `GRUBBER_SET` from the environment
(Automator sets neither) and to `~/bin/register`. They put `~/bin` on `PATH`, so
register finds its fileanchor engine there.

**Curated binder list.** If `~/.config/fileregister/binders` exists (one binder
per line, `#` comments), the picker shows only those, which keeps it short and
on topic. Keep the file current when you switch topics. Without it, the picker
lists every binder via `register list`.
