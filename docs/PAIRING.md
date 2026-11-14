# Genuine owner-UI pairing

## Manual workflow (supported)

1. Keep Windows, the gateway, and the phone awake, connected to the tailnet, and the original app open. Have the phone's **Paste link** field ready first: the native invitation lasts only about two minutes, and this tool cannot extend it. Open phone pairing in the **native owner UI**. Request a genuine one-time invite there. Do not search app state files, registry, credential stores, process memory or APIs for a control token.
2. Transfer an owner-created screenshot of that QR (or an explicitly owner-copied native invite text file) privately to the Mac. The original app's QR remains untouched. Never put it in the source tree except within gitignored `private/`.
3. Run, with a **new** output directory each time:

```sh
python3 tools/make-https-invite.py private/native-owner-qr.png \
  --origin https://phone.example.com --output-dir private/invites/session-1 --copy
```

Use your real HTTPS origin, not the example. For a private text file instead of an image, add `--input-link`. Without `--copy`, the clipboard is untouched. The tool compiles its own Swift source in a temporary directory, decodes locally, preserves the genuine token/code/name/HPKE public key, rejects unknown/duplicate fields and **any E2E descriptor**, and changes routing only. It advertises `hostname:443` plus a typed hosted HTTPS endpoint, then verifies QR round-trip.

The new output directory is 700 and `invite.txt`/`invite.png` are 600. Existing destinations are refused. Inputs and outputs expire with the native dialog's actual deadline, not a new deadline invented by this tool. Delete stale material manually.

Alternatively create a **gitignored owner-private JSON** file, e.g. `private/invite.private.json`, with `public_origin` and `output_dir` keys. Values are your HTTPS origin and a fresh output directory. Run `--config private/invite.private.json`. CLI origin/output flags override those settings. Relative output paths are relative to your shell's working directory. Never place a bearer token in the settings.

4. In the native phone app use **Paste link**, not a bare HTTPS URL. If using Universal Clipboard, understand its exposure and clear the clipboard afterwards. Keep the native owner UI active until the phone reports ready.
5. Turn off phone Wi-Fi, leave the tailnet connected, and verify reconnection on cellular. Confirm a harmless Windows-local conversation belongs to **This computer**, not another gateway/shared workspace.
6. Exercise separately approved native permissions manually. Pairing is not proof that protected local application, screen, filesystem or terminal access works. Do not claim completion until the actual phone passes the checklist.

Optional macOS one-click entry: double-click the **new** `scripts/pair-phone.command` (or run it with Bash). It resolves the project relative to itself, reads `private/invite.private.json`, waits for phone readiness **before** asking for a fresh native screenshot, and requests explicit clipboard consent. Each attempt uses a new gitignored `private/invites/session-*` output directory, overriding the configured output directory for this launcher only. It sounds a local macOS alert when ready, displays the one-line link and terminal QR, then asks you to scan or paste immediately; there is no post-generation readiness delay. It does not install, replace or modify any existing Desktop launcher, automate the owner UI, or treat pressing Return as proof of successful pairing.

## Terminal QR and one-line link

The Mac launcher prints a **single logical-line invitation** and a QR directly in the terminal. Your terminal may visually soft-wrap the long URI; the script does not insert line breaks into it. In a direct iTerm2 session it renders a PNG with preserved aspect ratio, independent of font glyphs and line spacing. Otherwise it uses solid ANSI background cells (two spaces per module), never half-block glyphs. Both retain a white quiet zone and compact QR correction level L. The saved PNG remains available.

To redisplay a still-valid generated invitation without issuing another code:

```sh
python3 tools/show-invite.py private/invites/session-1/invite.txt
```

This is an explicit secret-display action: terminal output and scrollback contain the short-lived pairing credential. Do not record it, redirect it to service logs, attach it to an issue, or commit it. Default invite generation still withholds credential values.

The launcher compiles the local Swift renderer and checks terminal space **before** you open a fresh native pairing window. Direct iTerm2 image mode needs at least 64 columns and 28 rows and automatically fits a square image. The portable solid-cell mode needs substantially more space (typically 158 columns and 82 rows for a full invite); maximize or reduce font size when prompted. No external QR package is required. The renderer is cached under gitignored `.cache/`; only its source is published. If an unusually long invite needs more space, resize and redisplay the still-valid file; printing cannot extend native expiry.

`--format ansi` explicitly selects solid cells. `--format png` is only for a terminal known to support iTerm2 inline images. Automatic image selection is disabled under tmux/screen and for unknown terminal programs rather than changing their settings or assuming image passthrough. A nested TUI may therefore need solid mode or a direct iTerm2 window.

## Optional legacy automation intentionally omitted

The private project's owner-UI adapter was tested against **Cua Driver legacy CLI 0.23.2**. Its `call` interface, snapshot shape and UI labels are not a portable/current Cua guarantee. No adapter, fixed PID/window, SSH target, driver binary, automatic Cua update, profile bypass or machine-specific launcher is shipped here. Manual UI pairing avoids silently controlling the wrong generation/window. If adding an adapter later, require explicit private JSON for SSH alias, driver path/socket and fresh owner PID/window, with no actual-user defaults; respect the installed driver's profile approvals.
