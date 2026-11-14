#!/usr/bin/env python3
"""Explicitly display an owner-private invite as one logical line and terminal QR.
Terminal output/scrollback contains the short-lived credential. Never redirect it
into service logs, recordings, issues, or a public repository.
"""
import argparse
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path, nargs='?', help='private invitation text file')
    parser.add_argument('--check', action='store_true', help='compile the local renderer before minting')
    parser.add_argument('--preflight', action='store_true', help='check terminal space before minting')
    args = parser.parse_args()
    os.umask(0o077)
    binary = renderer()
    if args.input:
        read_link(args.input)
        measured = subprocess.run([str(binary), '--measure', str(args.input)],
            capture_output=True, text=True, check=True, timeout=15)
        required = json.loads(measured.stdout)
    else:
        required = {'columns': 79, 'rows': 40}
    if args.check and not args.preflight:
        print('Local terminal QR renderer ready; no invite displayed.')
        return 0
    if args.preflight:
        if sys.stdout.isatty():
            actual = shutil.get_terminal_size()
            columns, rows = max(80, required['columns']), required['rows'] + 3
            if actual.columns < columns or actual.lines < rows:
                print(f'Before generating: enlarge this terminal to at least {columns} columns × {rows} rows.')
                print('Maximize the window or reduce its font size. No pairing code has been requested.')
                return 2
        return 0
    if not args.input:
        raise ValueError('Choose a private invite file to display')
    link = read_link(args.input)
    # One newline terminates the URI; there are no inserted line breaks in it.
    print('ONE-LINE LINK (the terminal may visually soft-wrap):', flush=True)
    print(link, flush=True)
    if sys.stdout.isatty():
        actual = shutil.get_terminal_size()
        if actual.columns < required['columns'] or actual.lines < required['rows'] + 2:
            print(f'QR needs {required["columns"]} columns × {required["rows"] + 2} rows. Enlarge the terminal and rerun this display command; it does not mint another invite.')
            return 2
    print('\nScan in MausBot; single use, valid only until the native deadline:', flush=True)
    subprocess.run([str(binary), str(args.input)], check=True, timeout=15)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        print('Terminal QR display failed; private error details withheld.', file=sys.stderr)
        sys.exit(1)
