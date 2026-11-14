# Update, disable and uninstall

## Update only the bridge

1. Review native app/protocol changes; run the full test suite and rebuild separately. Never patch the native app. Verify the candidate SHA256 from the trusted build, not just its transfer destination.
2. Retain the source/scripts used to stage this installation. Inspect any previous `.previous` backup or `candidate.exe`; archive it privately outside the installation before the next update. Scripts refuse occupied backup/staging names, never force-overwrite them.
3. Keep the native app and owner phone-access UI active for the candidate probe. As the original installing owner:

```powershell
$hash = Read-Host 'Verified candidate SHA256'
.\scripts\update-binary.ps1 -NewBinary .\dist\openmausbot-tailnet-bridge.exe -ExpectedSHA256 $hash
```

The helper validates installation/task ownership, copies and rehashes the candidate privately, probes the genuine pipe, stops only the matching task, retains the previous bridge binary and restarts. A swap/start failure attempts rollback. A failed probe leaves the existing task unchanged and candidate for inspection. A task that stays running is not proof of successful phone access; run the acceptance checks.

If a later acceptance check fails: stop the verified task, archive the candidate and current bridge binary privately, restore only the known-good bridge `.previous` binary, then restart the same task. Inspect the ownership marker/action first. Do not overwrite other files, force-stop native processes, alter pairings or loosen authentication. If rollback fails, leave the bridge disabled and inspect locally.

## Disable / uninstall

Elevate PowerShell as the same installing owner for firewall removal:

```powershell
.\scripts\uninstall-windows.ps1
```

Both task and firewall ownership are validated before removal. Mismatches abort. This stops/removes only the bridge task and exact narrow rule. A separately launched bridge must be closed manually; unrelated processes are never killed. Files, configuration and logs are retained for inspection, as are all native app files and pairings. Delete only the dedicated standalone directory manually after reviewing it.

On the Linux gateway remove only your uniquely named bridge router/service/entrypoint configuration, not shared routers, certificates, compose files or unrelated listeners. Remove only the DNS record you created after checking ownership; revoke bridge-specific DNS credentials only if they are not shared. Delete stale pairing images/links and clear clipboards. Tailnet policy changes are likewise operator-owned.

Neither installation nor removal changes the Desktop launcher, shared server configuration, native app, execution policy or installed Cua version.
