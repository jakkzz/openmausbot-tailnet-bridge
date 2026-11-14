# Computer-to-computer pairing is a separate protocol

The current launcher is **phone/tablet -> Windows native companion only**. It does not mint desktop/server invitations. Never give a desktop client a companion phone code and claim that it is a server code.

For a native computer client connecting to another workspace server:

1. Choose a reachable, verified **workspace instance**, not just a machine found in tailnet peer status. Verify its `/.well-known/openmausbot/environment` identity through an operator-approved route.
2. The **target host** must issue a genuine server invitation through its supported owner UI or an already-authorized admin session. Ordinary SSH/loopback access is not desktop-owner authority.
3. The native server flow creates `POST /api/auth/pairing` with an explicit scope choice: `client` for chat/client access, or `admin` plus `client` only when the owner chooses administration. Respect organization pairing/remote-access policy.
4. Use the host-issued server link, shaped like `https://workspace.example.com/pair#code=OWNER_GENERATED_CODE`. The placeholder is not a real code. On the source desktop use **+ -> Connect to a server** and paste the genuine link.
5. A code does not make a loopback-only server reachable. The host's supported sharing route must already be configured and verified.

## Native Headscale limitation

In the inspected upstream v0.1.158 implementation, the native Tailscale computer-sharing flow accepts `.ts.net` names and gates its server pairing UI on a ready sharing origin. A custom Headscale DNS suffix can therefore prevent that UI from becoming available even though the VPN is connected. No independent, supported unattended owner mint interface was established for an unpaired desktop-owned installation.

The phone bridge in this repository is not that desktop server-sharing route: its upstream is the restricted companion protocol, not an unrestricted harness proxy. It does not provide desktop `/api/auth/pairing` authority.

An eventual general from/to CLI must use verified registered instance adapters and an explicitly owner-authorized server mint path; unsupported targets must be refused with a clear setup error. Discovery alone must not harvest credentials, read process memory, modify vendor code, or silently elevate scopes. This document describes requirements, **not an implemented automatic desktop-pairing feature**.
