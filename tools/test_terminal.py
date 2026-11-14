"""Terminal QR tests use synthetic invites and stdlib PNG reconstruction only."""
import base64
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import urlencode
import zlib

TOOLS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('show_invite', TOOLS / 'show-invite.py')
show = importlib.util.module_from_spec(spec)
spec.loader.exec_module(show)


def chunk(kind, data):
    return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data) & 0xffffffff)


def rasterize_terminal(text, path):
    lines = re.sub(r'\x1b\[[0-9;]*m', '', text).splitlines()
    width = len(lines[0])
    grid = []
    for line in lines:
        if len(line) != width or set(line) - {' ', '█', '▀', '▄'}:
            raise AssertionError('Unexpected terminal QR geometry or glyph')
        grid.extend([[0 if c in ('█', '▀') else 255 for c in line],
                     [0 if c in ('█', '▄') else 255 for c in line]])
    scale = 8
    pixels = b''.join(b'\0' + bytes(value for value in row for _ in range(scale))
                      for row in grid for _ in range(scale))
    header = struct.pack('>IIBBBBB', width * scale, len(grid) * scale, 8, 0, 0, 0, 0)
    path.write_bytes(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header)
                     + chunk(b'IDAT', zlib.compress(pixels)) + chunk(b'IEND', b''))
    return width, len(lines)


class LinkPresentationTests(unittest.TestCase):
    def test_terminal_preflight_before_any_invite(self):
        class TTY(io.StringIO):
            def isatty(self):
                return True
        for dimensions, status in [((80, 24), 2), ((80, 43), 0)]:
            out = TTY()
            with patch.object(show, 'renderer', return_value=Path('unused-renderer')), patch.object(sys, 'argv', ['show-invite.py', '--preflight']), patch.object(sys, 'stdout', out), patch.object(shutil, 'get_terminal_size', return_value=os.terminal_size(dimensions)):
                self.assertEqual(show.main(), status)
            if status == 2:
                self.assertIn('No pairing code has been requested', out.getvalue())

    def test_control_characters_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'input.txt'
            for value in ['not-an-invite', 'openmausbot://pair?token=a\n&code=b', 'openmausbot://pair?x=\x1b[2J']:
                p.write_text(value)
                with self.assertRaises(ValueError):
                    show.read_link(p)

    @unittest.skipUnless(sys.platform == 'darwin' and shutil.which('swiftc'), 'macOS Swift frameworks required')
    def test_terminal_roundtrip_and_one_line_link(self):
        with tempfile.TemporaryDirectory(prefix='terminal-qr-test-') as tmp:
            tmp = Path(tmp)
            key = base64.urlsafe_b64encode(b'\x04' + bytes(range(64))).decode().rstrip('=')
            routes = base64.urlsafe_b64encode(json.dumps([{'url': 'https://phone.example.com', 'kind': 'hosted', 'priority': 0}], separators=(',', ':')).encode()).decode().rstrip('=')
            link = 'openmausbot://pair?' + urlencode({'address': 'phone.example.com:443', 'token': 'omb_pair_' + 'A' * 43, 'code': '123456', 'name': 'Example', 'hosts': 'phone.example.com', 'endpoints': routes, 'secretKey': key})
            source = tmp / 'synthetic.txt'
            source.write_text(link)
            binary = show.renderer()
            measured = subprocess.run([str(binary), '--measure', str(source)], capture_output=True, text=True, check=True)
            dims = json.loads(measured.stdout)
            raw = subprocess.run([str(binary), str(source)], capture_output=True, text=True, check=True)
            png = tmp / 'terminal.png'
            width, rows = rasterize_terminal(raw.stdout, png)
            self.assertEqual((width, rows), (dims['columns'], dims['rows']))
            codec = tmp / 'codec'
            subprocess.run(['swiftc', str(TOOLS / 'phone-qr.swift'), '-o', str(codec)], check=True, capture_output=True, timeout=120)
            decoded = tmp / 'decoded.txt'
            subprocess.run([str(codec), 'decode', str(png), str(decoded)], check=True, capture_output=True, timeout=30)
            self.assertEqual(decoded.read_text(), link)
            display = subprocess.run([sys.executable, str(TOOLS / 'show-invite.py'), str(source)], capture_output=True, text=True, check=True, timeout=30)
            self.assertEqual([line for line in display.stdout.splitlines() if line.startswith('openmausbot://')], [link])
            self.assertIn('\x1b[30;47m', display.stdout)


if __name__ == '__main__':
    unittest.main()
