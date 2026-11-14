"""Synthetic terminal QR tests: solid cells, inline PNG and one-line output."""
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
    lines = text.splitlines()
    grid = []
    columns = None
    for line in lines:
        cells, color = [], None
        consumed = ''
        for match in re.finditer(r'\x1b\[([0-9;]*)m| +', line):
            consumed += match.group(0)
            if match.group(1) is not None:
                code = match.group(1)
                if code == '48;5;16': color = 0
                elif code == '48;5;231': color = 255
                elif code == '0': color = None
                else: raise AssertionError('Unexpected ANSI color')
            else:
                if color is None: raise AssertionError('Uncoloured cells')
                cells.extend([color] * len(match.group(0)))
        if consumed != line or not cells or len(cells) % 2:
            raise AssertionError('Expected solid two-column background modules, not glyphs')
        columns = len(cells) if columns is None else columns
        if len(cells) != columns or any(cells[i] != cells[i + 1] for i in range(0, columns, 2)):
            raise AssertionError('Unexpected terminal QR geometry')
        grid.append(cells[::2])
    scale = 8
    pixels = b''.join(b'\0' + bytes(value for value in row for _ in range(scale))
                      for row in grid for _ in range(scale))
    header = struct.pack('>IIBBBBB', columns // 2 * scale, len(grid) * scale, 8, 0, 0, 0, 0)
    path.write_bytes(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header)
                     + chunk(b'IDAT', zlib.compress(pixels)) + chunk(b'IEND', b''))
    return columns, len(lines)


class LinkPresentationTests(unittest.TestCase):
    def test_backend_selection_respects_multiplexers(self):
        self.assertEqual(show.display_format('auto', {'TERM_PROGRAM': 'iTerm.app'}), 'png')
        self.assertEqual(show.display_format('auto', {'TERM_PROGRAM': 'iTerm.app', 'TMUX': 'session'}), 'ansi')
        self.assertEqual(show.display_format('auto', {'TERM_PROGRAM': 'herdr'}), 'ansi')
        self.assertEqual(show.display_format('auto', {'TERM_PROGRAM': 'herdr', 'LC_TERMINAL': 'iTerm2'}), 'ansi')
        self.assertEqual(show.display_format('ansi', {'TERM_PROGRAM': 'iTerm.app'}), 'ansi')

    def test_terminal_preflight_before_any_invite(self):
        class TTY(io.StringIO):
            def isatty(self): return True
        for mode, dimensions, status in [('ansi', (80, 24), 2), ('ansi', (158, 82), 0), ('png', (64, 28), 0)]:
            out = TTY()
            with patch.object(show, 'renderer', return_value=Path('unused-renderer')), patch.object(sys, 'argv', ['show-invite.py', '--format', mode, '--preflight']), patch.object(sys, 'stdout', out), patch.object(shutil, 'get_terminal_size', return_value=os.terminal_size(dimensions)):
                self.assertEqual(show.main(), status)
            if status == 2: self.assertIn('No pairing code has been requested', out.getvalue())

    def test_control_characters_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'input.txt'
            for value in ['not-an-invite', 'openmausbot://pair?token=a\n&code=b', 'openmausbot://pair?x=\x1b[2J']:
                p.write_text(value)
                with self.assertRaises(ValueError): show.read_link(p)

    @unittest.skipUnless(sys.platform == 'darwin' and shutil.which('swiftc'), 'macOS Swift frameworks required')
    def test_terminal_roundtrip_and_one_line_link(self):
        with tempfile.TemporaryDirectory(prefix='terminal-qr-test-') as tmp:
            tmp = Path(tmp)
            key = base64.urlsafe_b64encode(b'\x04' + bytes(range(64))).decode().rstrip('=')
            routes = base64.urlsafe_b64encode(json.dumps([{'url': 'https://phone.example.com', 'kind': 'hosted', 'priority': 0}], separators=(',', ':')).encode()).decode().rstrip('=')
            link = 'openmausbot://pair?' + urlencode({'address': 'phone.example.com:443', 'token': 'omb_pair_' + 'A' * 43, 'code': '123456', 'name': 'Example', 'hosts': 'phone.example.com', 'endpoints': routes, 'secretKey': key})
            source = tmp / 'synthetic.txt'; source.write_text(link)
            binary = show.renderer()
            measured = subprocess.run([str(binary), '--measure', str(source)], capture_output=True, text=True, check=True)
            dims = json.loads(measured.stdout)
            raw = subprocess.run([str(binary), str(source)], capture_output=True, text=True, check=True)
            png = tmp / 'terminal.png'
            self.assertEqual(rasterize_terminal(raw.stdout, png), (dims['columns'], dims['rows']))
            codec = tmp / 'codec'
            subprocess.run(['swiftc', str(TOOLS / 'phone-qr.swift'), '-o', str(codec)], check=True, capture_output=True, timeout=120)
            decoded = tmp / 'decoded.txt'
            subprocess.run([str(codec), 'decode', str(png), str(decoded)], check=True, capture_output=True, timeout=30)
            self.assertEqual(decoded.read_text(), link)
            for mode in ['ansi', 'png']:
                display = subprocess.run([sys.executable, str(TOOLS / 'show-invite.py'), str(source), '--format', mode], capture_output=True, text=True, check=True, timeout=30)
                self.assertEqual([line for line in display.stdout.splitlines() if line.startswith('openmausbot://')], [link])
                if mode == 'png':
                    self.assertIn('preserveAspectRatio=1', display.stdout)
                    match = re.search(r'\x1b\]1337;File=[^:]+:([A-Za-z0-9+/=]+)\x07', display.stdout)
                    self.assertIsNotNone(match)
                    inline = tmp / 'inline.png'; inline.write_bytes(base64.b64decode(match.group(1)))
                    width, height = struct.unpack('>II', inline.read_bytes()[16:24])
                    self.assertEqual(width, height)
                    check = tmp / 'inline-decoded.txt'
                    subprocess.run([str(codec), 'decode', str(inline), str(check)], check=True, capture_output=True, timeout=30)
                    self.assertEqual(check.read_text(), link)
                else:
                    self.assertIn('\x1b[48;5;', display.stdout)
                    self.assertNotIn('▀', display.stdout)


if __name__ == '__main__': unittest.main()
