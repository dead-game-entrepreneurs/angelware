# OutRun 2006 PS2 support

The target build is US PS2 `SLUS-212.74`, `SYSTEM.CNF` version 1.01. Support is experimental:
Protocol tests cover accounts, bootstrap, room creation and search, stats and game isolation.
The retail client has completed DNAS, signup, keyed login, an empty Quick Game search,
room creation and a room update. Two unmodified PS2 clients have discovered a room, joined the
same lobby and completed a race over native UDP 3658. Both post-race result rows were saved.
The retail ranking screen has read both saved rows back. The optional PNACH transport has also
passed automatic hosting, discovery/join, a completed race and both result writes on two stock
Flatpak PCSX2 clients in Sockets mode. Synchronization quality, broader network coverage and
ghost transfers still need testing. DemonWare relay/adaptive modes remain rejected because they
are a different transport. The PSP build `ULUS-10064` is a separate target and is not enabled.

## Endpoints and identity

| Endpoint | Service |
| --- | --- |
| `auth.outrun2.mmp.demonware.net` | TCP 3074, title ID 182 |
| `lsg.outrun2.mmp.demonware.net` | TCP 3074, lobby gateway |
| `outrun2.stun.eu.demonware.net` | UDP discovery and traversal |
| `outrun2.stun.us.demonware.net` | UDP discovery and traversal |

Generic `stun.{eu,us,jp,au}.demonware.net` names also occur in the SDK. Use a reachable address
separate from WWE when both games use UDP 3074/3075. The disc includes `DNAS310.IMG`; AngelWare
does not replace DNAS.

## Protocol layouts

Addresses are ELF virtual addresses. Network code is directly in the main ELF; extraction does
not require WWE's overlay unpacking. Disc binaries and extracted executables stay in ignored local work directories.

Auth setup at `0x12ab3c` uses port 3074 and passes title 182 at `0x12ab74`. Signup
`0x151170` uses request 0, the fixed `DEADBEEF` key and a 96-byte credential block. Login
`0x151868` uses request 10. Replies at `0x151a20` accept success 700 and the same keyed/plain
128-byte ticket structures as WWE; the unpacker is `0x152078`. Signup failure handling at
`0x12bc88` distinguishes status 707 (`0x2c3`, existing username) and 709 (`0x2c5`). The server
returns 707 for duplicate signup. TCP framing at
`0x2c1720` and `0x2c2060` matches the shared transport. The lobby task factory at `0x159328`
writes slot, sequence, task, arguments and a trailing raw u32.

Discovery uses padded version 1: IP request constructor/serializer `0x163528`/`0x163540`, NAT
serializer `0x163888`. The existing v1 discovery implementation handles these layouts.

Session serialization at `0x121b80` has the shared address[25], ID[8], key[16] and four slot
counts, then one i32, host and lobby strings capped at 16 bytes, and thirteen i32 values.
Search serialization at `0x129cb8` writes kind, requested count and thirteen criteria. The menu
builder at `0x1242b0` converts negative criteria to `INT_MAX`, the wildcard. Mapping criteria to
the thirteen session integers follows their serialized order; exact menu semantics and retail
search results still require in-game confirmation. Full and private-only rooms are excluded.

Service 8 is a bootstrap query in this build. Login submits the default query with no serialized object fields via
`0x158d68`; `0x12a530` calls its completion parser with a null result-object pointer. The handler
acknowledges that query and rejects WWE profile layouts. This build does not use WWE profiles.

## Statistics and ghosts

Stats writer `0x159a70` sends u8, i32 board, u64 target, then the game row. It omits WWE's flag
byte. The row serializer `0x122600` writes a u64 rating, six u32 columns and an optional i64
ghost-file ID. The decoder `0x122800` reads the six columns as i32; replies preserve that shape.
The ghost ID is stored as eight exact bytes to preserve all 64 bits. File operations reuse the
SDK service 10 upload, remove and download tasks, with game-scoped ownership and visibility.

The custom ranking query at `0x122e18` writes kind/count, u32 board, u64 user pivot and u32 mode.
The current implementation supports user-centered reads. Exact mode semantics and the complete
retail board catalogue remain to be established. Ratings default to descending order;
`options.ascending_boards` explicitly selects lower-first boards. No unverified board numbers
are assigned an inferred direction.

The completed two-client race wrote board 231 through service 4/task 1. Both writes decoded
successfully and were verified in PostgreSQL under `game_id = 'outrun2006'`: ratings 1466 and
1434, each repeated in the first of six columns with five zero columns. Neither row carried a
ghost ID. The retail ranking screen then submitted service 4/task 5 for board 231; the server
returned two rows with total two, and the displayed ranking appeared correct to the tester.
This validates the write and range-read layouts for that board. Units, the complete board
catalogue, other query modes and each board's correct ranking direction remain to be established.

## Native room probes

After receiving a room, the retail client probes the advertised host on UDP 3658 before showing
it. The live two-PC test confirmed that the server returned a room with a valid 23-field layout
and available slots, followed by repeated 13-byte probes from the guest. No replies were received
when the local PCSX2 socket backend had no host listener on UDP 3658. Fedora's active workstation
zone already allowed that port. Bridged networking gives the emulated console its own reachable
address; a socket backend needs a usable inbound mapping before it can answer the native probe.

WWE's adaptive relay and encrypted address rewrite are not applied to OutRun. Its auth and lobby
crypto being compatible does not establish compatibility of the peer transport or address rewrites.

The native probe is 13 bytes: a two-byte peer tag, type `0x12`, then IPv4, little-endian u16
port and alternate IPv4. Builder `0x123488` sends it to a fixed destination port 3658.
Handler `0x222cf8` reads the embedded return address and replaces its port with the observed
source port; it does not replace the embedded IP with the observed source IP. Address reader
`0x21f980` and sender `0x225d48` confirm that layout and address selection. On the local test
machine, discovery advertised the server alias `.250`, while native probes actually left from
`.141`. That mismatch must be considered separately from whether the host receives the probe.

A simulated DemonWare introduction was tested against both emulators. Each returned a matching
29-byte `0x0c` echo from its existing DemonWare endpoint, rather than from native UDP 3658.
The lobby notification consumer `0x124c20` processes friend requests and session invitations;
an invitation is queued for the UI rather than automatically starting a native connection.
No automatic server notification that opens the native hosting socket has been established.

Use `python3 tools/outrun_peer_capture.py CAPTURE.pcap` to summarize native probes, embedded
addresses and DemonWare traversal separately without printing ticket or session-key payloads.
`--since` accepts an ISO timestamp with a timezone. A capture made on the server PC shows only
traffic visible there; it cannot establish delivery inside a remote emulator. Check the remote
host's UDP listener and active firewall zone before attributing a missing reply to either one.
One diagnostic native probe from the server alias to the hosting laptop received an ICMP
port-unreachable error. That establishes rejection of UDP 3658 at that moment, but a remote
listener/firewall check was needed to distinguish a closed socket from an explicit reject.
The host's `ss` output then confirmed no UDP 3658 listener, while its active FedoraWorkstation
zone allowed UDP 1025–65535.

### Successful Sockets test

The local server alias `.250` used `.250` as the preferred source for local discovery traffic,
while the emulator's native socket was bound to `.141`. A temporary preferred-source route
made discovery report `.141`, matching native traffic. This is a development-host routing fix,
not a change to the game's protocol or to PCSX2. It is not persisted across host reboots:

```sh
sudo ip route replace local 192.168.2.250/32 dev br0 table local proto kernel scope host src 192.168.2.141
```

With the laptop still hosting, the desktop restarted the game, logged in, tried Quick Game,
and immediately created its own room. The laptop then left its room and searched within two
minutes. This opened the desktop's native socket toward the laptop before it became the host.
The capture showed a native probe reply, join exchange and sustained traffic through a complete
race in both directions, followed by both result writes. Both clients and emulators remained unmodified. This demonstrates a working direct
path after outbound priming; it does not demonstrate automatic hosting or a working relay.

The native dispatcher `0x227cb0` supports several messages in one datagram. Message type flags
`0x20` and `0x40` select trailing u8 and u16 lengths; the base type masks out those flags.
The capture summarizer reports these boundaries and numeric types without printing game payloads.

### Automatic hosting audit

The normal host path `0x120dc0` consumes the create-room reply, copies its ID/key and calls
native initialization `0x21dfb0`. That initializer creates the local player and local addresses;
it does not send a packet or introduce a remote peer. The native update loop `0x228780` flushes
queued traffic for existing peers, excluding the local player from broadcasts. The audited
lobby notification path supplies no automatic native-peer introduction. Changing a create-room
reply's addresses is therefore insufficient to make a fresh host send from UDP 3658.

In the tested PCSX2 Sockets implementation, `SocketAdapter::SendUDP` creates host UDP sockets
on the first outbound packet. `UDP_FixedPort::Recv` then admits incoming traffic only through
a matching child session. This matches the observed absent 3658 listener, ICMP port-unreachable
response and successful outbound-priming test. Forwarding a native probe through AngelWare
cannot create the missing emulator socket. Ordinary router forwarding also cannot deliver a
packet into a nonexistent host socket.

PCSX2's unmerged [inbound UDP proposal](https://github.com/PCSX2/pcsx2/pull/14285) documents the
same requirement and proposes pre-binding configured ports. It is not a setting available in
the tested build, and no emulator changes were applied. Automatic hosting with unmodified
binaries needs a networking backend that can deliver the first inbound frame, or another
verified stock-client trigger for outbound native traffic. No such trigger has been established
for this title. DemonWare relay/adaptive configuration remains rejected. The optional game patch described
below opens the native outbound socket and uses a separate transport.

## Experimental PNACH relay

See [installation and testing](../../../patches/outrun2006/README.md). Only US build
`SLUS-21274`, CRC `38C16493`, is supported. `options.native_relay=true` adds a separately managed
UDP 3658 listener while the SDK discovery service remains in `direct` mode. The emulator and
ISO remain unchanged; PNACH alters the running game in memory.

The patch captures the discovery server IP and the opaque AngelWare login token after a
successful auth response. Revision 2 hooks the central network pump `0x21dd18` (called from frame dispatcher
`0x1078c8`), sends REGISTER packets from the actual native socket before room creation,
consumes ACKs even without a native worker, and keeps the mapping alive while online.
One bounded pending-packet slot preserves a native datagram if this pump receives it
before the game's receiver. Revision 1 hooked native worker `0x228780`, which the retail
create test established is not scheduled before hosting: login/discovery state was valid,
but registration/tick counters stayed at zero and the create request never reached the server.
The new regression executes menu frames and ACK handling without invoking a native receiver. A relay ACK
assigns a unique `10.254.x.y` address. The SDK's own local/public CommonAddr entries use that
virtual address before the create-session API runs. Native send/receive hooks wrap complete
native packets and reconstruct the original game-facing source address and two-byte peer tag.
This avoids both the absent inbound socket and public-IP equality shortcuts on one LAN/NAT.
Offline/LAN and raw discovery calls continue through the original functions.

AOR1 datagrams have a 32-byte header: magic at 0, little-endian u32 kind at 4, opaque token[16]
at 8, network-order virtual IPv4 at 24, little-endian u32 payload length at 28, then up to 2048
native bytes. Kinds are REGISTER=1, DATA=2, ACK=3, DELIVER=4. DATA names the virtual destination;
ACK names the recipient; DELIVER names the virtual source and carries the recipient's token.
Only the server's discovered IPv4 and port 3658 are accepted by the patch. No WWE encryption
or address rewriting is applied. The native payload remains unencrypted, as in the stock game;
the token is a bearer capability and must not be printed or included in public captures.

A lobby wrapper authorizes a token only after the gateway has decoded an encrypted task with
that session's key. UDP registration must come from that connection's public IP. DATA must
come from its registered endpoint; REGISTER can refresh/rebind the port at the same public IP.
The relay forwards only between authenticated clients disclosed to the same matchmaking room.
Room deletion, host disconnect, and client disconnect revoke the relevant admission. Mappings
expire after 30 seconds without REGISTER. Frames, clients, and packet rates are bounded.

Revision 3 also expands the game's `sceInsock` per-thread staging buffer to 4096 bytes:
verified constructor literals at `0x13d3f4` (capacity) and `0x13d408` (capacity + 127-byte
allocation/alignment overhead). The original constructor derives the matching aligned IOP
RPC allocation from that capacity. This preserves support for full 2048-byte native packets
plus the envelope. Revision 2's retail counters showed successful REGISTER delivery but no
ACK processing: `sceInsockRecvFrom` (`0x13ea88`) rejects a request exceeding its configured
capacity before reading the datagram. The test harness now enforces that behavior on both
send and receive, reproduces the old failure and verifies the matching expanded allocation.

The revision 3 single-client retail check passed on stock Flatpak PCSX2 2.8.2: the loaded
PNACH matched the generated instructions, all three socket contexts had 4096-byte capacity,
and the desktop registered as `10.254.0.4` through its real `192.168.2.141:3658` endpoint.
While still in the online menu it received four ACKs, set `ready=1`, and reported zero native
drops/read errors. The desktop then created a room directly, without a prior Quick Game
search. The server acknowledged service 5/task 1, then task 2; both requests advertised
`10.254.0.4:3658` as the local and public host address. The host retained relay readiness,
with twelve ACKs and zero drops/read errors during the check. This verifies automatic
pre-host registration, return traffic, and room creation. The laptop then registered as
`10.254.0.5` from `192.168.2.46:3658`; its first Quick Game search returned the one room,
and the tester confirmed a successful join. The desktop had sent 99 and received 105
native datagrams through the shim, with zero drops/read errors and 34 ACKs.

Both players then completed a race. At `2026-11-15T22:33:42Z`, service 4/task 1 saved board 231
for both accounts: `eeeeee` rating 1448 with `{1448,0,0,0,0,0}`, and `ffffff` rating 1451 with
`{1451,0,0,0,0,0}`. Both replies succeeded, and PostgreSQL was queried independently to verify
the persisted rows. The desktop later reported 4,827 native sends, 2,686 native receives,
87 ACKs, zero shim drops/read errors and continued relay readiness. These counters cover the
session, including the room and race; they are not a measurement of end-to-end loss or latency.

The tester reported that the games might look slightly out of sync, then asked to defer
investigation until broader tester feedback. This remains an uncharacterized observation;
completion and successful result writes do not establish synchronization quality. Host swaps,
long idle hosting, additional players, different-network clients and ghosts need further checks.

The shim occupies verified zero padding at `0x3bd000` after the SNR2 symbol table, bounded below
`0x3c0000`. The builder checks the supported ELF hash, six original prologues, symbol-table size
and cave contents when supplied `--elf`. It emits code writes only; mutable token/keepalive
state is never overwritten by a repeating PNACH entry. Hook wrappers preserve full 128-bit EE
GPRs and HI/LO; compiled C uses 16-byte stack alignment and assembly adapters for the game's
extended argument ABI. Unicorn tests execute the compiled MIPS shim with mocked IOP APIs.
They validate framing, registration-before-create, keepalive, peer tags, source reconstruction,
malformed-packet rejection, offline/raw fallback and the hook boundary. They do not replace a
real PCSX2 race test.

## Retail validation

Use fresh accounts and rooms in two isolated PCSX2 instances. Validate DNAS, signup, keyed login,
bootstrap, hosting/search, joining, a complete race, post-race writes, leaderboard paging and a
ghost upload/download. Capture DNS, TCP 3074 and UDP traffic. A protocol test passing is not a
claim that this retail sequence has completed.
