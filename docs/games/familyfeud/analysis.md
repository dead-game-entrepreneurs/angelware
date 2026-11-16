# Family Feud PS2 support

Status: experimental implementation. Automated tests cover anonymous login,
encrypted lobby tasks, room lifecycle and game isolation. Two unmodified PCSX2
Sockets clients completed online games over direct P2P and forced SDK relay.
Broader network coverage, physical consoles and adaptive fallback remain to be
verified separately.

The local US disc boots `SLUS-214.46`, `SYSTEM.CNF` version 1.02. The main ELF
SHA-256 is `6d232ab60fc56c6688615560c211f0a746d4c1a89bc6cc02a8cdfde15306c6a5`.
Addresses below are virtual addresses in that executable. Extracted files and
Ghidra projects remain in ignored `work/familyfeud/`.

## Comparison

| Area | Family Feud | WWE SvR 2007 | OutRun 2006 |
| --- | --- | --- | --- |
| Network code | Main ELF | LBYM/NET overlays | Main ELF |
| SDK identification | `DemonWare-ps2-1.74-full-matchmaking` | DemonWare 1.75 | DemonWare auth/lobby, native gameplay |
| Title ID | 521 (`0x209`) | 4669 | 182 |
| Observed static login path | Anonymous request `0x10` | Account request `0x0a` | Account request `0x0a` |
| Discovery | Packed v2 | Packed v2 | Padded v1 |
| Room fields after common seven | Name and two i32 values | Wrestling settings | Name/lobby and racing settings |
| Peer transport | DemonWare connection/socket router and bdDTLS v2 | DemonWare plus game-specific match transport | Native UDP 3658 |

Family Feud is a stronger candidate for reusing the generic DemonWare relay than
OutRun. That does not establish compatibility with WWE's address rewriter,
adaptive observer or game-specific match transport.

## Endpoints and login

| Hostname | Role |
| --- | --- |
| `familyfeud.auth.mmp.demonware.net` | Auth, TCP 3074 |
| `familyfeud.lsg.mmp.demonware.net` | Lobby, TCP 3074 |
| `familyfeud.stun.us.demonware.net` | Discovery/traversal |
| `familyfeud.stun.eu.demonware.net` | Discovery/traversal |

The game also uses generic `stun.{eu,us,jp,au}.demonware.net` during startup,
before title login. The retail PCSX2 log confirmed failure at these lookups when
only the title-specific domains were redirected. Redirect all four generic names
as well; they are required for this startup path, not merely optional fallbacks.
The disc includes SCE EENet networking modules and DNAS code. DNAS remains a
separate dependency. The title-specific DNS overrides are listed in the root README.

Startup `0x19a830` resolves auth, constructs its endpoint with port 3074, creates
the auth service with title 521 (`0x19ac04`), then calls `0x264b38`. This invokes
builder `0x2662e8`: message type `0x10`, an untyped bit buffer containing u32
random seed and u32 title ID. There is no username, password or persistent user
identifier in this request. Success is 700; the reply parser `0x2667e0` accepts
the plaintext-ticket response `0x11` and reads two 128-byte blocks.

The SDK also contains account signup/login builders, including account request
`0x0a` at `0x266130`, but their presence does not mean the game uses them.
The traced startup uses anonymous auth. Lobby connection setup is `0x267380`.
The task factory `0x263a10` uses the familiar service message, slot/sequence,
task byte, arguments and trailing raw u32 structure. Tiger and 3DES primitives
are present (`0x352430` derives an IV from a serialized seed;
`0x3525b0`/`0x352638` wrap encryption/decryption with 24-byte keys).

The shared auth implementation supports `0x10` through an explicit per-title
guest-login capability. It issues a fresh random, temporary identity and both
tickets without persisting a password account. WWE and OutRun reject anonymous
login, preserving their account verification. Shared accounts cannot be selected
from this request alone; a future linking flow needs additional information
outside this packet.

## Rooms and search

`CustomMatchMakingInfo` serializes at `0x1964d8` and parses at `0x1965a8`.
Its base serializer `0x2627a8` writes the same seven common fields:

```text
blob address[25], blob room ID[8], blob key[16],
i32 public maximum, i32 public used, i32 private maximum, i32 private used
```

The three additional fields are a NUL-terminated session-name string and two
i32 values. The name setter `0x196418` caps the name at 16 characters; the
receiver reads at most 16 bytes including any terminator, so generated/replied
names should leave space for termination until retail behavior is verified.

Create setup `0x19be58` sets slots to `2,1,0,0`, the game integers to 5 and 101,
and the session name from the caller. The meaning of those integers is not yet
established. Do not import WWE or OutRun field interpretations.

Service 5 tasks map to create/update/delete/find-by-ID/search as in the shared
matchmaking service (`0x261f80` through `0x262290`). The menu's search at
`0x19c650` builds the public-open query: i32 kind 2, i32 maximum results 20,
i32 required public openings 1. Query serialization is `0x261dc8` plus
`0x1959e8`. Other linked query classes include game-type and find-by-ID.
The shared room lifecycle and create-result ID/key handling look reusable;
the Family Feud adapter implements schema validation and public-open filtering.

The SDK contains stats code, but this pass has not established a retail stats,
profiles, friends or storage workflow. Do not enable WWE's handlers just because
their SDK classes are linked.

## Discovery and peer traffic

IP request constructor `0x287190` sets kind `0x1e` and version 2. Its serializer
`0x2871a8` writes one kind byte followed immediately by the two-byte version:
`1e 02 00`. It does not write the in-memory alignment byte. This matches WWE's
packed v2 request. The parser also contains legacy-v1 fallback handling.

The game starts `bdNet` (`0x27c400`), installs its connection handler, and joins
through the SDK connection store: `0x19bbd0` supplies the discovered room ID,
key and host address to `0x249e30`. The linked bdDTLS header constructor
`0x28f4d0` sets wire version 2; `0x28f4f8` serializes kind, version, tag and
sequence. DATA encrypt/decrypt paths `0x28e418`/`0x28eaf8` use the SDK crypto.

Game messages use this connection abstraction. For example, `0x19c950` sends
join-accepted type `0x1d` without an application payload; `0x19ca38` sends
join-rejected `0x1e`. Receiver `0x19d518` also dispatches synchronization,
play/pass, answer, family data, buzz timing and quit messages. The empty accept
message is encouraging because WWE's accept requires embedded address rewriting;
it does not prove that every other Family Feud packet is address-free.

Two unmodified PCSX2 Sockets clients completed games on both direct and forced
SDK-relay paths. The direct game used one introduction and zero relay offers.
The forced-relay game carried 73 packets / 3136 bytes in one direction and
74 packets / 3149 bytes in the other; both counters stopped advancing at game end.
No Family Feud-specific packet rewriting or PNACH was needed. Adaptive mode was
restored after that test; the timeout-triggered adaptive fallback still needs its
own retail check. Do not enable WWE's room-completion observer for this title
without evidence for Family Feud's report semantics.

## One public IP and domain routing

The original DNS name is absent from raw DemonWare TCP/UDP packets. Auth's title
ID can route TCP, and lobby tickets can retain game scope. DNS query history
cannot reliably identify a later UDP packet, especially behind shared NAT.

WWE and Family Feud have identical discovery requests, so the
`stun.share_with` selector cannot distinguish them. That feature deliberately
requires distinct wire versions and a discovery-only child; Family Feud also
uses the SDK peer transport. Merely adding another hostname will not extend it.

The implemented `stun.share_transport_with` option joins a common SDK router,
including its discovery, traversal, relay capacity and routing policy. Set it to
`svr2007` with the same bind/public address to share WWE's packed-v2 transport.
Native-transport titles, mismatched wire versions, overrides and alias chains are
rejected. See `configs/single-ip.example.json` for WWE, Family Feud and OutRun
on one IPv4. OutRun keeps its separate v1 discovery router and native UDP relay.

Generic UDP packets have no title identity. Auth/lobby tickets and room lookup
remain game-scoped. A peer rewrite hook must verify its own room/key binding;
WWE's hook leaves unknown rooms unchanged. This is common network infrastructure,
not routing based on source IP or DNS history. Independent routing policies
still need separate listeners. Retail same-IP coexistence remains to be tested.

## Local test setup

Enable `familyfeud` in the JSON runtime configuration, either with its own
UDP 3074/3075 listener or `"stun": { "share_transport_with": "svr2007" }`.
Forward the four title-specific and four generic SDK names above to that listener's public address.
Use the unmodified US game; no PNACH is required by the implementation.

The default routing mode is adaptive when Family Feud owns its listener; a
shared transport inherits its owner's mode. Both clients need a fresh boot after
changing discovery topology, so cached endpoints do not bypass the intended test.
The adapter registers only service 5 and the traced kind-2 public-open search;
other linked SDK services/query classes are not enabled.
