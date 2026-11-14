# Installation (operator-run)

Nothing here auto-deploys. Do not run against an existing installation without reviewing ownership. Example addresses are **not** discovery or real defaults:

| Role | Example |
| --- | --- |
| Windows | 100.64.0.10 |
| Linux gateway | 100.64.0.20 |
| HTTPS name | phone.example.com |

## 1. Prepare privately

Join the intended hosts/phone to your Tailscale-compatible tailnet. Replace the example addresses with actual tailnet addresses and write a least-privilege tailnet policy. Keep the hosts awake. Install/open the genuine Windows app normally; do not modify it. Determine its real executable path using normal OS properties.

Use Go 1.27.4 (or `go.mod` toolchain auto-selection), Python 3.10+, and Windows PowerShell 5.1+ for installation. macOS QR tooling additionally needs Apple Command Line Tools (`swiftc`, Vision, AppKit and CoreImage). Windows management scripts need the built-in ScheduledTasks and NetSecurity modules.

## 2. Build separately

On Linux/macOS:

```sh
go test -race -timeout 120s ./...
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-H=windowsgui' -o dist/openmausbot-tailnet-bridge.exe .
GOOS=windows GOARCH=amd64 go test -c -o dist/bridge.test.exe .
shasum -a 256 dist/openmausbot-tailnet-bridge.exe
```

On Windows, set `$env:GOOS='windows'; $env:GOARCH='amd64'`, create `dist`, and run the same Go commands without the POSIX variable prefix. Run `dist\bridge.test.exe -test.v -test.timeout=120s` on Windows; fixtures use only test-owned processes and random pipe names. Verify the transferred executable's SHA256 against the trusted build hash.

## 3. Configure and stage on Windows

Copy the source package and built binary into a separate operator working directory. Keep that source directory for management scripts. Copy `examples/bridge.windows.example.json` to a gitignored `private/bridge.windows.json` and edit **all four fields**:

- `listen`: Windows literal tailnet IP, port 18798 (IPv6 must be bracketed).
- `gateway_ip`: gateway's actual literal source IP as Windows sees it, not a subnet.
- `public_origin`: bare ASCII DNS HTTPS origin, no explicit port, credentials, query or path.
- `expected_exe`: genuine absolute local-drive app executable path. Not the bridge executable.

The configuration contains infrastructure metadata and belongs in private owner-controlled storage even though it contains no API token. Do not add fields or credentials to it.

From the source directory, as the intended Windows owner:

```powershell
$hash = Read-Host 'Verified build SHA256'
.\scripts\stage-windows.ps1 -Binary .\dist\openmausbot-tailnet-bridge.exe -Config .\private\bridge.windows.json -ExpectedSHA256 $hash
```

Default destination: `%LOCALAPPDATA%\OpenMausBotTailnetBridge`; an existing directory is refused. Its contents inherit only owner/SYSTEM access. A failed staging attempt leaves files for manual inspection rather than deleting potentially unrelated data. You may pass a new absolute `-Root` consistently to all scripts; avoid reparse points and unusual quoted paths.

Open the native owner pairing/phone-access UI, then probe locally:

```powershell
$root = Join-Path $env:LOCALAPPDATA 'OpenMausBotTailnetBridge'
& (Join-Path $root 'openmausbot-tailnet-bridge.exe') -probe -config (Join-Path $root 'bridge.windows.json')
```

No device token is required for this health check. A failure is not permission to use TCP fallback.

## 4. Install the Windows task and firewall rule

Elevate PowerShell **as the same owner** (UAC; not a different admin account). Run:

```powershell
.\scripts\install-windows.ps1
```

This creates a limited, interactive-at-logon task executing the standalone GUI executable directly, plus a TCP18798 firewall rule for the configured local IP, gateway peer and bridge program. Existing task/rule names are refused. Partial task/firewall creation rolls back. It does not grant native app permissions or change script execution policy; follow your organization's signing/approval policy rather than disabling it. Review pre-existing broader firewall rules separately.

## 5. Configure the Linux gateway and DNS

Use a dedicated Traefik instance or manually merge the examples into your operator-managed instance **without overwriting shared configuration or unrelated routers**.

1. Copy `examples/traefik-static.example.yml` and `examples/traefik-dynamic.example.yml` into new, uniquely named operator files. Replace IPs/domain/email, paths, DNS provider and resolver names.
2. Bind TCP443 only to the gateway's tailnet address. With containers, the example assumes host networking; otherwise explicitly publish only that host IP and verify the real source IP reaching Windows. Do not allow a public wildcard listener or trust client-supplied forwarding headers.
3. Set the file-provider directory to your new bridge-only rule directory. Keep router/service/middleware names unique. The dynamic example preserves Host, streams events immediately and allows the 105s native save budget.
4. Configure generic ACME **DNS-01** using Traefik's documented integration for your DNS provider. Provision least-privilege credentials entirely outside this project via your operator secret manager. Create protected ACME storage (600, writable by Traefik). No provider token path or registrar helper is prescribed.
5. Add a DNS-only (not traffic-proxied) A record for your hostname pointing to the gateway's tailnet IP. Add AAAA only if that complete IPv6 route works. DNS records and certificate transparency may reveal the hostname/IP; private DNS with a domain you control is another option. Public HTTP-01 validation is not needed.
6. Restrict the gateway firewall to tailnet ingress. Tighten the example source ranges to the intended phone/peers. Do not add router forwards, public proxying or third-party relays.
7. From an allowed tailnet peer verify the certificate and `https://YOUR_NAME/api/health`. Never use `curl -k`. Confirm Windows sees the gateway's configured peer address.

Traefik must supply exactly one `X-Forwarded-Proto: https`, retain Host, and avoid logging private URLs/headers/bodies. Check socket bindings and tailnet policy independently. Finally follow [pairing](PAIRING.md) and [validation](VALIDATION.md).
