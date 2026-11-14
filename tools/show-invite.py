#!/usr/bin/env python3
"""Explicitly display a private one-line invite and a terminal QR.
Prefer an inline PNG in a direct iTerm2 session; otherwise use solid ANSI
background cells, never font-dependent half-block glyphs. Output/scrollback
contains a short-lived credential: do not record, publish, or log it.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

TOOLS = Path(__file__).resolve().parent


def renderer():
    source = TOOLS / 'terminal-qr.swift'
    digest = hashlib.sha256(source.read_bytes()).hexdigest()[:16]
    cache = TOOLS.parent / '.cache' / 'terminal-qr'
    cache.mkdir(mode=0o700, parents=True, exist_ok=True)
    binary = cache / ('renderer-' + digest)
    if not binary.exists():
        with tempfile.TemporaryDirectory(prefix='build-', dir=cache) as temp:
            compiled = Path(temp) / 'renderer'
            subprocess.run(['swiftc', str(source), '-o', str(compiled)], check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=120)
            compiled.chmod(0o700)
            os.replace(compiled, binary)
    return binary


def read_link(path):
    value = path.read_text(encoding='utf-8').strip()
    if (not value.startswith('openmausbot://pair?') or len(value) > 8192
            or any(ord(c) < 33 or ord(c) > 126 for c in value)):
        raise ValueError('Not a single-line native invite')
    return value


def display_format(requested, env=None):
    if requested != 'auto':
        return requested
    env = os.environ if env is None else env
    # Do not assume a multiplexer forwards image escapes, or change its settings.
    program = env.get('TERM_PROGRAM', '')
    direct = not env.get('TMUX') and not env.get('STY')
    iterm = program == 'iTerm.app' or (not program and env.get('LC_TERMINAL') == 'iTerm2')
    return 'png' if direct and iterm else 'ansi'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path, nargs='?', help='private invitation text file')
    parser.add_argument('--format', choices=['auto', 'ansi', 'png'], default='auto')
    parser.add_argument('--check', action='store_true', help='compile the local renderer before minting')
    parser.add_argument('--preflight', action='store_true', help='check terminal space before minting')
    args = parser.parse_args()
    os.umask(0o077)
    binary = renderer()
    mode = display_format(args.format)
    if args.input:
        read_link(args.input)
    required = {'columns': 158, 'rows': 79}
    if mode == 'ansi' and args.input:
        measured = subprocess.run([str(binary), '--measure', str(args.input)],
            capture_output=True, text=True, check=True, timeout=15)
        required = json.loads(measured.stdout)
    if args.check and not args.preflight:
        print('Local terminal QR renderer ready; no invite displayed.')
        return 0
    if args.preflight:
        if sys.stdout.isatty():
            actual = shutil.get_terminal_size()
            columns, rows = (64, 28) if mode == 'png' else (required['columns'], required['rows'] + 3)
            if actual.columns < columns or actual.lines < rows:
                print(f'Before generating: enlarge this terminal to at least {columns} columns × {rows} rows ({mode} QR).')
                print('Maximize the window or reduce its font size. No pairing code has been requested.')
                return 2
        return 0
    if not args.input:
        raise ValueError('Choose a private invite file to display')
    link = read_link(args.input)
    print('ONE-LINE LINK (the terminal may visually soft-wrap):', flush=True)
    print(link, flush=True)
    actual = shutil.get_terminal_size(fallback=(80, 28))
    if mode == 'ansi' and sys.stdout.isatty():
        if actual.columns < required['columns'] or actual.lines < required['rows'] + 2:
            print(f'Solid QR needs {required["columns"]} columns × {required["rows"] + 2} rows. Enlarge the terminal and rerun this display command; it does not mint another invite.')
            return 2
    print('\nScan in MausBot; single use, valid only until the native deadline:', flush=True)
    if mode == 'png':
        with tempfile.TemporaryDirectory(prefix='private-terminal-qr-') as temp:
            png = Path(temp) / 'pairing.png'
            subprocess.run([str(binary), '--png', str(args.input), str(png)], check=True, timeout=15)
            data = png.read_bytes()
            width = min(68, max(32, actual.columns - 4))
            height = min(34, max(8, actual.lines - 9))
            payload = base64.b64encode(data).decode('ascii')
            # iTerm2 renders image pixels independently of font and line spacing.
            # Bounding box + preserveAspectRatio prevents stretching the QR.
            print(f'\x1b]1337;File=inline=1;size={len(data)};width={width};height={height};preserveAspectRatio=1:{payload}\x07', flush=True)
    else:
        subprocess.run([str(binary), str(args.input)], check=True, timeout=15)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        print('Terminal QR display failed; private error details withheld.', file=sys.stderr)
        sys.exit(1)
