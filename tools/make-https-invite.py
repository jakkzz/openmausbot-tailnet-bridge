#!/usr/bin/env python3
"""Route a genuine owner-issued invite via HTTPS; never discover credentials."""
import argparse
import base64
import binascii
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.parse import parse_qs, quote, urlencode, urlsplit


class InviteError(ValueError):
    """Safe, fixed diagnostics: never include private input."""


def https_origin(value):
    try:
        u = urlsplit(value)
        host = u.hostname
        if (u.scheme != "https" or not host or u.netloc != host
                or u.path not in ("", "/") or u.query or u.fragment
                or "?" in value or "#" in value or not value.isascii()
                or any(c.isspace() for c in value)):
            raise ValueError()
        try:
            ipaddress.ip_address(host)
        except ValueError:
            pass
        else:
            raise ValueError()
        labels = host.split(".")
        if len(host) > 253 or len(labels) < 2 or any(
                not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
                for label in labels):
            raise ValueError()
        return "https://" + host
    except (ValueError, TypeError, AttributeError):
        raise InviteError("A bare ASCII DNS HTTPS origin (default port) is required") from None


def rewrite_invite(link, origin):
    origin = https_origin(origin)
    try:
        u = urlsplit(link)
        if u.scheme != "openmausbot" or u.netloc != "pair" or u.path or u.fragment:
            raise ValueError()
        raw = parse_qs(u.query, strict_parsing=True, keep_blank_values=True)
    except ValueError:
        raise InviteError("Not a native companion invite") from None
    allowed = {"address", "token", "code", "name", "hosts", "endpoints", "secretKey", "e2e"}
    if set(raw) - allowed or any(len(v) != 1 for v in raw.values()):
        raise InviteError("Unknown or duplicate invite fields; review vendor protocol")
    q = {k: v[0] for k, v in raw.items()}
    if "e2e" in q:
        raise InviteError("Pinned E2E requires a native compatible route; no downgrade performed")
    if (not re.fullmatch(r"omb_pair_[A-Za-z0-9_-]{43}", q.get("token", ""))
            or not re.fullmatch(r"[0-9]{6}", q.get("code", ""))):
        raise InviteError("Invalid owner-issued native invite")
    if "secretKey" in q:
        key = q["secretKey"]
        try:
            data = base64.urlsafe_b64decode(key + "=" * (-len(key) % 4))
        except (ValueError, binascii.Error):
            raise InviteError("Invalid pinned public key") from None
        if not re.fullmatch(r"[A-Za-z0-9_-]{87}", key) or len(data) != 65 or data[0] != 4:
            raise InviteError("Invalid pinned public key")
    host = urlsplit(origin).hostname
    q["address"] = host + ":443"
    q["hosts"] = host
    endpoints = [{"url": origin, "kind": "hosted", "priority": 0}]
    q["endpoints"] = base64.urlsafe_b64encode(
        json.dumps(endpoints, separators=(",", ":")).encode()).decode().rstrip("=")
    for k in ("token", "code", "name", "secretKey"):
        if k in raw and q.get(k) != raw[k][0]:
            raise InviteError("Non-routing trust field changed")
    return "openmausbot://pair?" + urlencode(q, quote_via=quote)


def write_private(path, value):
    # Exclusive creation prevents overwriting unrelated files or following symlinks.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as out:
        out.write(value)


def run(command, **kwargs):
    # Never expose compiler/codec errors, which might include private paths.
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL,
                   stderr=subprocess.DEVNULL, timeout=120, **kwargs)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("input", type=Path, help="owner-UI screenshot, or private link file with --input-link")
    p.add_argument("--input-link", action="store_true")
    p.add_argument("--origin", help="required HTTPS origin, unless supplied by --config")
    p.add_argument("--config", type=Path, help="gitignored owner-private JSON settings")
    p.add_argument("--output-dir", type=Path, help="new private directory; existing destinations are refused")
    p.add_argument("--copy", action="store_true", help="explicitly copy the private link using macOS pbcopy")
    a = p.parse_args()
    os.umask(0o077)
    settings = {}
    if a.config:
        settings = json.loads(a.config.read_text(encoding="utf-8"))
        if not isinstance(settings, dict) or set(settings) - {"public_origin", "output_dir"}:
            raise InviteError("Unknown private settings")
    origin = https_origin(a.origin or settings.get("public_origin"))
    destination = a.output_dir or settings.get("output_dir")
    if not destination:
        raise InviteError("Choose a new --output-dir (or configure output_dir)")
    out = Path(destination).expanduser().absolute()
    # No Cua update, shell profile changes, API calls or control-token discovery.
    with tempfile.TemporaryDirectory(prefix="bridge-invite-") as tmp:
        tmp = Path(tmp)
        codec = tmp / "phone-qr"
        run(["swiftc", str(Path(__file__).with_name("phone-qr.swift")), "-o", str(codec)])
        if a.input_link:
            native = a.input.read_text(encoding="utf-8").strip()
        else:
            decoded = tmp / "native.txt"
            run([str(codec), "decode", str(a.input), str(decoded)])
            native = decoded.read_text(encoding="utf-8").strip()
        link = rewrite_invite(native, origin)
        # Existing directories are deliberately not reused, even after a failed run.
        out.mkdir(mode=0o700, parents=True, exist_ok=False)
        textfile, png = out / "invite.txt", out / "invite.png"
        write_private(textfile, link)
        run([str(codec), "encode", str(textfile), str(png)])
        check = tmp / "roundtrip.txt"
        run([str(codec), "decode", str(png), str(check)])
        if check.read_text(encoding="utf-8").strip() != link:
            raise InviteError("QR round trip failed; do not use output")
        if (textfile.stat().st_mode & 0o777) != 0o600 or (png.stat().st_mode & 0o777) != 0o600:
            raise InviteError("Private output permissions failed; do not use output")
        if a.copy:
            with textfile.open("rb") as f:
                run(["pbcopy"], stdin=f)
    print("Verified owner-private HTTPS invite prepared" + (" and copied" if a.copy else ""))


if __name__ == "__main__":
    try:
        main()
    except InviteError as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, TypeError, subprocess.SubprocessError):
        print("Invite preparation failed; private details withheld. Inspect inputs and tool setup locally.",
              file=sys.stderr)
        sys.exit(1)
