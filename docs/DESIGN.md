# Design walkthrough

## Why a separate bridge?

Some native phone clients accept only a narrow class of private HTTP names. A privately resolved, publicly trusted DNS HTTPS name provides a compatible route without changing app files, authentication settings, native credential stores, or pairing records.

The operator-chosen Linux gateway accepts tailnet-only HTTPS. Windows listens on its literal tailnet address, port 18798, and accepts only that gateway's source IP. A trusted gateway supplies the original Host and an HTTPS marker. The HTTP hop is carried inside the tailnet tunnel; it is not application-layer E2E.

## Request path and trust

1. Traefik binds only its tailnet address, checks actual peer IP, and terminates the certificate.
2. Windows firewall restricts destination/program/port/source. Tailnet policy should allow only the intended peers.
3. Go checks exact peer IP, configured Host (optional :443), exactly one HTTPS marker, no browser Origin, and an `/api/` path. Browser CORS is not enabled.
4. Each request obtains a new native pipe connection, not TCP port 8810. It enumerates the generation-specific namespace and fails closed on ambiguity, probe errors or visible namespace changes.
5. Before sending HTTP it checks actual pipe server PID, sidecar parent PID, same-user SIDs, canonical executable paths, creation ordering and pinned process liveness. Both handles stay pinned. Exit monitoring closes the connection; writes recheck liveness.
6. Native bearer authentication and route permissions still decide access. Caller-controlled forwarding and private `x-openmausbot-*` authority headers are removed.

A path/SID match does not establish a signed binary or protect against arbitrary code running as the same user or administrator.

## Response handling

Only successful pair and endpoint snapshots and `companion.endpoints` SSE announcements are normalized. Hosted endpoint routing becomes the configured HTTPS origin. Pair responses lose LAN hosts to prevent bearer fallback to an unrelated Wi-Fi host. Unknown/non-routing JSON fields, token, device identity and HPKE public key values remain intact. Ordinary SSE frames are byte-preserved.

Malformed, duplicate, oversized, compressed or incompatible routing metadata is withheld. Pinned E2E metadata is unsupported and fails closed. Native non-2xx authentication responses are not rewritten. Response-header budgets are 30s normally and 105s for native secret-card saves; event bodies have no artificial whole-stream deadline. Gateway headers allow 110s.

## Ownership and lifecycle

The bridge has a separate user-private directory and limited interactive scheduled task that executes the GUI-subsystem binary directly. No PowerShell runtime wrapper or one-off task migration is needed. Installation refuses occupied names; management validates an installation marker and resource properties before changes. Backups are never force-overwritten.

DNS credentials stay entirely operator-owned. The examples do not include a registrar helper, credential location inventory, shared-server paths or native app modifications. Manual owner-UI invitation creation is the only supported source of pairing material.
