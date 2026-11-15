#!/usr/bin/env python3
"""Read only OutRun PNACH status through stock PCSX2 PINE (no tickets/payloads).

Run from this checkout after building the PNACH. PINE must already be enabled.
Supports the normal Unix socket and the running Flatpak's private mount namespace.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import struct

ROOT = Path(__file__).resolve().parents[1]

class Pine:
    def __init__(self, path):
        self.socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.socket.settimeout(3)
        self.socket.connect(str(path))

    def receive(self, size):
        data = b''
        while len(data) < size:
            part = self.socket.recv(size-len(data))
            if not part:
                raise RuntimeError('PINE connection closed')
            data += part
        return data

    def words(self, addresses):
        addresses = list(addresses)
        payload = b''.join(struct.pack('<BI', 2, address) for address in addresses)
        self.socket.sendall(struct.pack('<I', len(payload)+4)+payload)
        size, = struct.unpack('<I', self.receive(4))
        if size < 5 or size > 65536:
            raise RuntimeError('Invalid PINE reply size')
        reply = self.receive(size-4)
        if reply[0] or len(reply) != 1+4*len(addresses):
            raise RuntimeError('PINE read failed (is the game running?)')
        return struct.unpack('<'+'I'*len(addresses), reply[1:])

    def word(self, address):
        return self.words([address])[0]

def socket_path(explicit, pid):
    if explicit:
        return Path(explicit)
    path = Path(os.environ.get('XDG_RUNTIME_DIR', f'/run/user/{os.getuid()}'))/'pcsx2.sock'
    if path.exists():
        return path
    if pid is None:
        candidates = []
        for process in Path('/proc').iterdir():
            try:
                if process.name.isdigit() and (process/'comm').read_text().strip() == 'pcsx2-qt':
                    candidates.append(int(process.name))
            except (OSError, ValueError):
                pass
        if len(candidates) != 1:
            raise RuntimeError('Pass --pid for one PCSX2 instance or --socket for its PINE socket')
        pid = candidates[0]
    return Path(f'/proc/{pid}/root/run/user/{os.getuid()}/pcsx2.sock')

def status(pine, build_dir):
    pnach = ROOT/'patches/outrun2006/SLUS-21274_38C16493.pnach'
    expected = {}
    for line in pnach.read_text().splitlines():
        if line.startswith('patch='):
            _, cpu, address, kind, value = line.split(',')
            if cpu == 'EE' and kind == 'word':
                expected[int(address, 16)] = int(value, 16)
    addresses = list(expected)
    actual = {}
    for start in range(0, len(addresses), 256):
        batch = addresses[start:start+256]
        actual.update(zip(batch, pine.words(batch)))
    mismatch = [f'{a:08X}' for a in addresses if actual[a] != expected[a]]
    result = {'patch_matches_build': not mismatch}
    if mismatch:
        result['instruction_mismatches'] = mismatch[:12]
        result['note'] = 'Full stop/boot required for the current PNACH, or another build is running. State was not read.'
        return result
    symbols = json.loads((build_dir/'symbols.json').read_text())
    base = symbols['state']
    # Deliberately skip token[16] at +24 and the pending native payload buffer.
    values = pine.words(range(base, base+24, 4))
    result.update(zip(('server', 'virtual_ip', 'authenticated', 'ready', 'ticks', 'last_ack'), values))
    for name in ('server', 'virtual_ip'):
        result[name] = socket.inet_ntoa(struct.pack('<I', result[name]))
    result.update(zip(('registers', 'acks', 'sent', 'received', 'dropped', 'pending_size'), pine.words(range(base+40, base+64, 4))))
    result['read_errors'] = pine.word(base+76+2048)
    result['online'] = bool(pine.word(0x374824))
    contexts = []
    pointer = pine.word(0x359598)
    seen = set()
    while 0x100000 <= pointer <= 0x1ffffa0 and pointer not in seen and len(contexts) < 16:
        seen.add(pointer)
        thread, capacity = pine.words([pointer+8, pointer+0x58])
        contexts.append({'thread': thread, 'buffer_capacity': capacity})
        pointer = pine.word(pointer)
    result['socket_contexts'] = contexts
    return result

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--socket')
    parser.add_argument('--pid', type=int)
    parser.add_argument('--build-dir', type=Path, default=ROOT/'work/outrun2006/pnach-build')
    args = parser.parse_args()
    try:
        pine = Pine(socket_path(args.socket, args.pid))
        try:
            print(json.dumps(status(pine, args.build_dir), indent=2))
        finally:
            pine.socket.close()
    except (OSError, RuntimeError, KeyError, ValueError) as error:
        parser.exit(1, f'{error}\n')
