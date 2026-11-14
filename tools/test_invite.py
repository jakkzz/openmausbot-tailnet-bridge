"""Synthetic fixtures only. Never contact a native app or remote machine."""
import base64
import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from urllib.parse import parse_qs, urlencode, urlsplit

spec = importlib.util.spec_from_file_location("invite", Path(__file__).with_name("make-https-invite.py"))
invite = importlib.util.module_from_spec(spec)
spec.loader.exec_module(invite)


class InviteTests(unittest.TestCase):
    def fields(self):
        return {
            "address": "192.0.2.18:8810", "token": "omb_pair_" + "A" * 43,
            "code": "123456", "name": "Example Windows",
            # Synthetic, untrusted test bytes, NOT an owner key.
            "secretKey": base64.urlsafe_b64encode(bytes([4]) + bytes(64)).decode().rstrip("="),
        }

    def link(self, fields):
        return "openmausbot://pair?" + urlencode(fields)

    def test_route_only_preserves_native_trust(self):
        original = self.fields()
        q = parse_qs(urlsplit(invite.rewrite_invite(self.link(original), "https://phone.example.com/")).query)
        for k in ("token", "code", "name", "secretKey"):
            self.assertEqual(q[k], [original[k]])
        self.assertEqual(q["address"], ["phone.example.com:443"])
        self.assertEqual(q["hosts"], ["phone.example.com"])
        encoded = q["endpoints"][0]
        routes = json.loads(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))
        self.assertEqual(routes, [{"url": "https://phone.example.com", "kind": "hosted", "priority": 0}])

    def test_fail_closed(self):
        for changes in ({"e2e": "{}"}, {"e2e": ""}, {"unknown": "x"}, {"token": "fake"},
                        {"code": "not-a-code"}, {"secretKey": "bad"}):
            with self.subTest(changes=changes), self.assertRaises(invite.InviteError):
                invite.rewrite_invite(self.link({**self.fields(), **changes}), "https://phone.example.com")
        with self.assertRaises(invite.InviteError):
            invite.rewrite_invite(self.link(self.fields()) + "&token=duplicate", "https://phone.example.com")

    def test_origin(self):
        for value in (None, "http://phone.example.com", "https://100.64.0.10", "https://[::1]",
                      "https://user:pass@phone.example.com", "https://phone.example.com:443",
                      "https://phone.example.com/path", "https://phone.example.com?",
                      "https://phone.example.com#x", "https://phone.example.com\n",
                      "https://bad_host.example.com", "https://-bad.example.com"):
            with self.subTest(value=value), self.assertRaises(invite.InviteError):
                invite.https_origin(value)

    def test_private_exclusive_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp) / "invite.txt"
            invite.write_private(target, "synthetic")
            if os.name != "nt":
                self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o600)
            with self.assertRaises(FileExistsError):
                invite.write_private(target, "replacement")
            self.assertEqual(target.read_text(), "synthetic")
            if os.name != "nt":
                link = Path(tmp) / "symlink"
                link.symlink_to(target)
                with self.assertRaises(FileExistsError):
                    invite.write_private(link, "replacement")


if __name__ == "__main__":
    unittest.main()
