"""Summarize OutRun UDP captures without printing keys or account payloads."""
import argparse
import datetime
import json
import socket
import struct

from svr_capture import packets


def native_messages(payload):
    """Read native message boundaries, without decoding application bodies.

    SLUS-212.74 dispatcher 0x227cb0 consumes a trailing length for messages
    carrying flag 0x20 (u8) or 0x40 (u16 LE). Multiple messages share a datagram.
    """
    position, end = 2, len(payload)
    messages = []
    while position < end:
        raw = payload[position]
        flag = raw & 0x60
        if flag == 0x20:
            end -= 1
            if end <= position:
                return None
            size = payload[end]
        elif flag == 0x40:
            end -= 2
            if end <= position:
                return None
            size = int.from_bytes(payload[end:end + 2], 'little')
        elif flag == 0:
            size = end - position
        else:
            return None
        if size < 1 or position + size > end:
            return None
        messages.append({'type': f'0x{raw & 0x9f:02x}', 'bytes': size})
        position += size
    return messages if position == end and messages else None


def describe(payload, native_port=False):
    # DemonWare traversal can target 3658 during diagnostics. Distinguish its
    # packed v2 envelope from the game's native, untyped packet format.
    if len(payload) == 29 and payload[1:3] == b'\x02\x00':
        names = {10: 'introduction', 11: 'forwarded introduction',
                 12: 'introduction echo', 14: 'keepalive'}
        if payload[0] in names:
            return {'protocol': 'demonware', 'kind': names[payload[0]]}
    if len(payload) == 13 and payload[2] == 0x12:
        # SLUS-212.74: 0x123488 builds this probe; 0x222cf8 answers it.
        # The native sender writes its peer tag in network order, then copies
        # an address consisting of IPv4, little-endian port, alternate IPv4.
        return {'protocol': 'native', 'kind': 'reachability probe',
                'peer_tag': struct.unpack_from('!H', payload)[0],
                'advertised_ip': socket.inet_ntoa(payload[3:7]),
                'advertised_port': struct.unpack_from('<H', payload, 7)[0],
                'alternate_ip': socket.inet_ntoa(payload[9:13])}
    if native_port:
        messages = native_messages(payload)
        if messages:
            return {'protocol': 'native', 'kind': 'transport',
                    'peer_tag': struct.unpack_from('!H', payload)[0],
                    'messages': messages}
    return {'protocol': 'unclassified', 'kind': f'{len(payload)}-byte UDP'}


def summarize(path, since=0):
    flows = {}
    for timestamp, protocol, endpoints, _, payload in packets(path):
        if protocol != 'udp' or timestamp < since:
            continue
        native_port = any(port in (3658, 3659, 3660)
                          for port in (endpoints[1], endpoints[3]))
        metadata = describe(payload, native_port)
        key = (endpoints, len(payload), json.dumps(metadata, sort_keys=True))
        if key not in flows:
            source, source_port, target, target_port = endpoints
            flows[key] = dict(source=f'{source}:{source_port}',
                              target=f'{target}:{target_port}',
                              bytes=len(payload), count=0, first=timestamp,
                              last=timestamp, **metadata)
        row = flows[key]
        row['count'] += 1
        row['last'] = timestamp
    rows = sorted(flows.values(), key=lambda row: row['first'])
    for row in rows:
        for field in ('first', 'last'):
            row[field] = datetime.datetime.fromtimestamp(
                row[field], datetime.timezone.utc).isoformat()
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('capture')
    parser.add_argument('--since', help='ISO timestamp with timezone')
    args = parser.parse_args()
    since = 0
    if args.since:
        parsed = datetime.datetime.fromisoformat(args.since)
        if parsed.tzinfo is None:
            parser.error('--since requires a timezone, for example +00:00')
        since = parsed.timestamp()
    rows = summarize(args.capture, since)
    print(json.dumps({'udp_packets': sum(row['count'] for row in rows),
                      'flows': rows}, indent=2))


if __name__ == '__main__':
    main()
