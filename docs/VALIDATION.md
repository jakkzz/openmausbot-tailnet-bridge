# Validation and acceptance

## Reproducible checks

```sh
go test -race -timeout 120s ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-H=windowsgui' -o /tmp/bridge-check.exe .
GOOS=windows GOARCH=amd64 go test -c -o /tmp/bridge-fixture-check.exe .
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -v
python3 -m py_compile tools/*.py
```

On Windows: `go test -v -timeout 120s ./...` (or the compiled fixture executable with `-test.timeout=120s`). The subprocess fixture has its own bounded timeout and test-owned cleanup. It tests impostor rejection with zero application bytes, child exit, parent exit while server survives and generation-bound closure. It does not enumerate/kill the native app.

The **inactive CI template** describes Ubuntu race tests, Windows full tests with explicit timeouts (including fixtures), Windows amd64 cross-build, Python compile/unit checks and PowerShell parsing. Its macOS job runs source-built Swift codec/synthetic invite round-trip tests. Builds are not published or uploaded. No active workflow or hosted CI result is claimed.

## Activating CI

The template is stored at `docs/ci-workflow.example.yml`, not in the active workflows directory. Publishing credentials did not have GitHub's `workflow` permission; that restriction was respected rather than expanding account scopes. An owner who wants CI can copy the template to `.github/workflows/test.yml` using appropriately authorized GitHub access, review the jobs, and enable Actions. Do not use a token workaround or paste credentials into issues or chat.

## Export validation scope

Locally run during export: Go race suite on macOS, Windows amd64 program and fixture cross-compilation, Python unit/compile checks, Swift codec/CLI synthetic round-trip, PowerShell syntax parsing and example-config validation, Bash launcher syntax, Go vet (host and Windows), and YAML parsing. See the handoff report for any check unavailable locally. CI configuration is not a claim that hosted jobs have run.

Production Go logic is copied unchanged. Test-only fixtures use example addresses/domains/usernames. Swift tooling adds private initial permissions, argument validation and overwrite refusal; Python and Windows management scripts are generalized separately.

## Human acceptance checklist (not claimed complete)

- Trusted TLS verification, private listener binding and no public exposure.
- Allowed gateway works; direct non-gateway peer rejected.
- Origin-bearing browser request denied; uncredentialed protected endpoint remains native 401/403.
- No credentials/URLs/payloads in bridge or gateway diagnostics.
- Missing/expired native generation returns unavailable; no reusable TCP fallback.
- Genuine phone pairing, authenticated snapshot/SSE, reconnect after generation restart.
- Cellular reconnect with tailnet on and Wi-Fi off.
- Correct Windows-local workspace, independent native permissions; protected native-app access tested explicitly.
- Safe limited-task login restart, update rollback and uninstall in a disposable Windows environment.

Never use private screenshots or credentials as CI fixtures. Any live integration or protected app access remains operator/human work and is not established by these tests.
