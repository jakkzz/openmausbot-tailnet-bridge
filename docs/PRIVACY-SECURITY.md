# Privacy and security

## Trust model

- **The gateway/Traefik terminates TLS and sees plaintext:** pairing/device bearer values, API data, files and streams may be visible there. Trust and secure both hosts, the tailnet administrator, DNS/certificate operator, and backups. Do not describe this as E2E.
- Tailnet membership is not native authentication. Tighten tailnet ACLs/grants to the intended phone, gateway and Windows host; the broad example gateway allowlist is only a starting point.
- Keep all hosts awake and connected. Windows must be logged in as the installing owner, with the original app open and phone access active.
- Native user permission prompts remain mandatory. Protected native-app access, screen, terminal, browser or filesystem control has **not been proven** by this public package or its synthetic tests.
- Same-user malware or an administrator can defeat path/SID trust. There is no executable-signature attestation.
- Host/forwarded-HTTPS markers are assertions from the trusted gateway, not independent TLS evidence. Never expose the Windows HTTP listener publicly.

## Fail closed, do not work around

The bridge has no TCP origin fallback. It refuses browser Origins, wrong peer/Host/scheme, unverifiable or ambiguous pipe candidates, incompatible route metadata and pinned E2E descriptors. Never remove the descriptor, switch off encryption, discover an API control token, read a credential store, or modify the app to make pairing work. A pinned HPKE public key in an ordinary native invite is preserved; it is not a license to downgrade a distinct E2E transport.

Native authentication denials pass through unchanged. Only successful routing fields are normalized. Future protocol changes require a fresh security review, not an expanding allowlist without tests.

## Sensitive material

Pairing screenshots, one-time links/codes, output PNGs/text, device tokens and private configuration are sensitive. Use owner-only storage, do not print them, and never attach them to issues. The tool creates a fresh output directory (700) and files (600), refuses existing destinations, and only copies to clipboard with explicit `--copy`. Clipboard managers and Universal Clipboard expand exposure; clear the clipboard after use.

The bridge's normal status diagnostics contain fixed labels, HTTP pairing status and startup addresses, not request URLs, headers, bodies or credentials. `-probe` is an owner-local diagnostic and may report OS pipe/executable details; do not publish its raw output. Existing logs/backups must remain private. Disable Traefik access logs (or implement reviewed redaction), debug capture and payload tracing; certificate state also belongs in protected operator storage.

No DNS provider credentials are supplied, discovered or managed here. Use least-privilege DNS-01 credentials and operator-managed secret injection. Never check them into this package.

## Maintenance and reporting

Experimental and unsupported by the vendor. Native app or OS changes can invalidate assumptions about pipes, invites or endpoints. Run the tests and manual acceptance checklist after updates, retain a bridge-only rollback, and stop rather than weakening security.

Report only a minimal synthetic reproduction privately to the owner through a channel you already trust. No dedicated security contact or disclosure SLA is claimed. Do not post live configs, user paths, native screenshots, tokens, logs, or packet captures.
