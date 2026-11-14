"""macOS-only source build and synthetic QR/CLI checks; no native app required."""
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from urllib.parse import parse_qs, urlencode, urlsplit


@unittest.skipUnless(sys.platform == "darwin" and shutil.which("swiftc"), "macOS Swift frameworks required")
class QRTests(unittest.TestCase):
    def test_cli_roundtrip_and_private_outputs(self):
        tools = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory(prefix="bridge-qr-test-") as tmp:
            tmp = Path(tmp)
            source = tmp / "synthetic.txt"
            # Deliberately synthetic credentials, never an owner-issued invite.
            fields = {"token": "omb_pair_" + "A" * 43, "code": "123456", "name": "Example"}
            source.write_text("openmausbot://pair?" + urlencode(fields))
            source.chmod(0o600)
            out = tmp / "output"
            command = [sys.executable, str(tools / "make-https-invite.py"), str(source),
                       "--input-link", "--origin", "https://phone.example.com",
                       "--output-dir", str(out)]
            result = subprocess.run(command, capture_output=True, text=True, timeout=120)
            self.assertEqual(result.returncode, 0, result.stderr)
            for secret in fields.values():
                self.assertNotIn(secret, result.stdout + result.stderr)
            self.assertEqual(stat.S_IMODE(out.stat().st_mode), 0o700)
            for name in ("invite.txt", "invite.png"):
                self.assertEqual(stat.S_IMODE((out / name).stat().st_mode), 0o600)
            q = parse_qs(urlsplit((out / "invite.txt").read_text()).query)
            self.assertEqual(q["token"], [fields["token"]])
            self.assertEqual(q["address"], ["phone.example.com:443"])
            # Exercise screenshot decoding as well as CLI text input.
            second = tmp / "second"
            image_command = [sys.executable, str(tools / "make-https-invite.py"), str(out / "invite.png"),
                             "--origin", "https://phone.example.com", "--output-dir", str(second)]
            result = subprocess.run(image_command, capture_output=True, text=True, timeout=120)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((second / "invite.txt").read_text(), (out / "invite.txt").read_text())
            before = (out / "invite.txt").read_bytes()
            result = subprocess.run(command, capture_output=True, text=True, timeout=120)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual((out / "invite.txt").read_bytes(), before)
            self.assertNotIn(fields["token"], result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
