# World Series of Poker 2008: Battle for the Bracelets

Status: experimental support for accounts, unfiltered matchmaking, friends,
invitations and score-only leaderboards. Two unmodified PCSX2 Sockets clients
completed direct and forced-relay games. Retail checks also confirmed friend
request/accept, invitation-based joins, offline-stat upload and score readback.
The shipped PS2 menu requires reopening the inviting friend's options rather
than displaying a general invite popup. Restrictive detailed search and
persistent poker profiles remain unsupported; physical consoles, EyeToy/video
and automatic relay fallback after a failed direct attempt remain untested.

## Disc and endpoints

US PS2 `SLUS-216.86`, `SYSTEM.CNF` version 1.00. Main ELF SHA-256:
`026bfdfd350913b848648231a4948ce2fecf207e8e67236701f7b1bf1e1daa2e`.
All addresses below are virtual addresses in that executable. Disc extraction,
Ghidra projects and private research stay in ignored `work/wsop2008/`.

| Setting | Value |
| --- | --- |
| Game key | `wsop2008` |
| Title ID | 4963 (`0x1363`), initializer `0x15ac94` |
| Auth | `wsop2008-ps2-auth.mmp3.demonware.net`, TCP 3074 |
| Lobby | `wsop2008-ps2-lsg.mmp3.demonware.net`, TCP 3074 |
| Discovery | `stun.{eu,us,jp,au}.demonware.net`, UDP 3074/3075 |
| SDK clue | v1.80 evaluation endpoint strings; MMP3 production endpoints |

The disc links SCE INET/INETCTL networking and `DNAS310.IMG`. DNAS remains a
separate dependency. Evaluation endpoints are present in another configuration
branch; they are not required production DNS overrides.

## Comparison with existing adapters

| Area | WSOP 2008 | WWE / Family Feud |
| --- | --- | --- |
| TCP length | u32 LE | u16 LE |
| Account login | Request 10, shared credentials | WWE request 10; FF anonymous request 16 |
| Account-name ID | Tiger(lowercase ASCII name) first 8 bytes | WWE hashes original name |
| Client encrypted body prefix | HMAC-SHA1 truncated to four bytes | Fixed `0xDEADBEEF` |
| Task request header | Typed u8 task ID | Typed u8 slot, sequence, task ID |
| Task reply header | Typed u64 transaction ID | Typed u8 slot, sequence |
| Discovery | Packed v2 | Packed v2 |
| Matchmaking fields | Common seven + 13 i32 + name + 2 i32 | Game-specific subclasses |

For cross-game account testing, use an account originally created with a
lowercase username. Older mixed-case identities hash differently; this change
does not migrate or merge those accounts.

## Authentication and framing evidence

`0x335ff8` builds signup with the same 96-byte credential block, fixed key
`DEADBEEF` repeated four times plus eight zero bytes, Tiger(password) and Tiger
seed IV as the older games. `0x336b70` builds account login. `0x33ecf0` lowercases
ASCII names before Tiger hashing; `0x33eba8` and `0x33f280` handle optional
initiator credentials. `0x338390` unpacks the same 128-byte ticket layout,
including the 24-byte session key at offset 97. Replies use status 700 on success.

`0x338860` sends a four-byte frame length, encryption flag, optional seed, then
payload/ciphertext. For encrypted client frames, plaintext is four tag bytes,
one service byte, task payload and padding to an eight-byte block boundary. The
HMAC covers bytes from offset 5 to the end, excluding the service byte. The
padding byte is the low byte of the connection seed. HMAC key initialization
at `0x37d018` selects SHA-1 (`0x4e8188`); the final result length is four bytes.
3DES-CBC and Tiger(seed) IV remain unchanged.

The retail send routine was executed locally under Unicorn with EE LQ/SQ and
conditional-move instruction support, ring-buffer I/O mocked, and the HMAC/3DES
primitives substituted. It independently confirmed the hash range, padding and
32-bit length. A non-secret plaintext fixture is in the frame regression tests.
`0x338fa0` receives server replies and still accepts unencrypted frames or the
legacy fixed magic after decryption; its declared-length bound is `0x3bf6`.

The shared listener distinguishes dialects using registered auth title IDs or
issued lobby tickets. It does not infer a title from an IP or hostname. Issued
tickets pin both the game and dialect. Existing game defaults remain legacy.

## Lobby bootstrap and matchmaking

`0x33af60` first sends an eight-byte raw preamble: u32 SDK version 180 and
u32 receive-buffer capacity (15360 in the retail trace). It then sends framed
hello 7 with a typed bit buffer containing two u32 values and 1024 raw ticket
bits. The listener validates the following ticket before accepting this preamble. `0x34a880` writes the task ID, appends the argument buffer
and adds a raw zero u32 terminator. `0x34ab20` reads a typed u64 transaction ID
and consumes pending requests in FIFO order. `0x340c90` reads status, echoed task
ID and result count for matchmaking; it does not add a second total-count field.

`0x315240` submits service 8 task 1 using the default query at `0x349dc8`, with
no serialized object fields and a null result pointer. The adapter acknowledges
only that verified shape and does not reuse WWE's persistent profile handler.

Room serializer/reader `0x1365d8` / `0x136878` use 23 fields:

- Common address[25], ID[8], key[16] blobs and four i32 slot counts.
- Thirteen i32 values from object offsets `0x24..0x54`.
- NUL-terminated name with 64 bytes of storage (63 content bytes).
- Two i32 values from offsets `0x9c` and `0xa0`.

Base serializer `0x341278` stores the slot counts as bytes before writing i32
fields. The adapter validates shapes, lengths and slot allocation, preserves
opaque game values, and assigns independent room IDs/keys through the existing
matchmaking service. Disconnect cleanup and ownership checks are shared.

The browse path at `0x17fcb0` uses query kind 2, maximum 20, two zero criteria
(serializer `0x1370f0`, constructor `0x137090`). Kind 1 serializes seven detailed i32 criteria (`0x1364a0`, constructor
`0x1371a8`). The retail unfiltered menu search uses `[1,20,MAX,MAX,MAX,MAX,MAX,MAX,1]`,
where MAX is `0x7fffffff`. Both unfiltered forms are supported, returning public
rooms with free seats in creation order; kind 1 also matches the final room
compatibility flag. Restrictive kind-1 filters are not fully recovered and return
no results rather than broadening the search. Unknown shapes return no results.

## Connectivity

`0x366f80` initializes discovery type 0x1e, version 2; `0x366f98` emits its packed
three-byte request. This shares the existing WWE/Family Feud SDK transport through
`stun.share_transport_with`. No WWE peer address rewriter is registered for WSOP.
Both direct gameplay and forced opaque relaying use UDP 3658 on the clients.
The game also constructs objects with port 33000; that port was not required in
the completed local games.

Discovery results are cached across login attempts. After a server restart,
fully stop/boot the clients without loading save states. Merely logging in again
can leave stale mappings and make a forced-relay test continue over direct P2P.

## Keepalive disconnect regression

The joiner did **not** finish the first game cleanly. Its gateway was closed by
AngelWare upon reading a zero u32 during gameplay, after which the host later
submitted a score and deleted the table. The user confirmed that the joiner was
disconnected. This is not evidence of a clean completed game.

`0x3396f0` sends a raw zero u32 after 40 seconds without outgoing data and has an
80-second receive watchdog. `0x339b10` consumes zero records without treating them
as application messages. The MMP3 reader now recognizes these keepalives; the
gateway echoes four zero bytes to refresh the client's receive watchdog. This
does not consume a task transaction or drop the host's room. Legacy zero-length
frames remain invalid. Automated tests interleave keepalives, encrypted tasks,
room creation and lookup, including a keepalive while hosting.

## Friends and notifications

The WSOP friend service is 9: propose 1 (`0x33c2e0`), accept/reject 2/3, remove 4,
list 5 (`0x33c750`), pending requests 19 (`0x33c4a0`). The retail failed request
carried context u8 0 and the other player's u64 ID. Friend-list rows use u64 ID,
name and bool online (`0x33d120`); request-list rows omit the bool (`0x33d5c0`).
These match the shared friend service, now registered for WSOP.

Mailbox fetch is service 6 task 1 (`0x3473d8`), with context, u32 offset/count and
two bools. The retail login uses `[0,0,100,false,true]`. Message dispatch is
`0x346db8`; base header decoder `0x380e50` reads u64 timestamp, u64 message ID,
u32 flags and bool. The friend sender decoder `0x349ac8` adds u64 sender and
name. Type 1's registered factory (`0x342420`, factory vtable `0x4b29f0`) creates
`0x348bd8`; its actual decoder is `0x3489d0`, which adds u16 data length and a
blob only when nonzero. This matches the existing mailbox/push encoding.

The adapter reuses the shared friend/mailbox implementations with game-scoped
storage and online presence. Automated encrypted-lobby tests cover proposal,
push, pending list, mailbox fetch/ack, acceptance, online/offline presence and
isolation from WWE's friend data. Retail request/accept and selecting a friend to
accept a game invitation were confirmed. The server sent the type-5 push to the
laptop's active gateway connection without a write error.

Invitation type 5 is registered at `0x342a30`: factory `0x4b29a0` creates
`0x348908`, whose decoder `0x348610` adds an eight-byte session blob and u16
optional-data length. Types 6/7 (answers) and 9/10 (presence) instead use the
sender-only factory `0x4b29c8` and decoder `0x349ac8`. WSOP supplies its own
notification codec so concatenated mailbox results remain aligned; the legacy
codec for other games is unchanged.

`0x31e578` sets notification state 2 and its timestamp for a live type-5 invite.
`OnlineServiceGetSpriteNotificationIcon` (`0x2245c0`) selects the invitation icon
from that state. Its timed branch stops returning it after 30 seconds. The disc's
actual menu source explains the missing general alert: `LiveNotificationMacro`
in the `DATA.PAK` file chunk at offset `0x10c4d15e` uses
`UpdateLiveNotificationIcon` and explicitly sets `OnlyOnPlatform = XBOX`.
The PS2 native notification getter is present in the ELF, but its presence alone
does not establish a visible PS2 alert. Packed menu sources stay in ignored
research files; assets use concatenated zlib streams, usually 16 KiB per stream.

A reverse-direction retail test confirmed that the desktop decoded the laptop's
invite: read-only PINE inspection showed service notification state 2, one pending
invitation from the laptop, and the live friend entry's invitation flag set.
The selected-friend snapshot still had its old flag until the user reopened that
friend's options. Reselecting exposed Accept Invite; the desktop sent service 9
task 9 and the user confirmed joining the lobby. This flow needs no further
server or game changes; reopening the friend options is necessary if they were
already selected when the invite arrived.

Retail WSOP accepts proposals without acknowledging their mailbox message. The
shared social handler now clears matching request notifications on resolution,
including repeated accept/reject attempts. Mailbox fetch also filters and clears
resolved proposals left by older server versions. Invitations to the same table from
the same sender are deduplicated, including after they are answered; a new table
can be invited immediately. Accept/reject clears its pending mailbox entry and
repeated answers are ignored. When an inviting player disconnects, their pending
invitations and transient deduplication state are removed. Automated tests cover
these flows without a separate mailbox acknowledgement, as well as concatenated
answer messages. The user confirmed that stale friend requests are fixed.

## Rankings and leaderboards

WSOP's custom row at `0x1fa960` / `0x1fa980` delegates to SDK serializer
`0x34c1f8` and reader `0x34bf08`. It serializes one signed i64 score; readback is
u64 player ID, i64 score, u64 rank and a name of up to 63 bytes. The trailing
in-memory fields at offsets 0x68/0x6c are UI state and are not serialized.
This is a score-only codec, with no WWE-specific statistic columns.

Write task 1 (`0x34b860`) is context u8 0, flag u8, i32 board, u64 user 0 and
i64 score. The captured host submitted board 11, flag 0, score 0. The adapter
stores rows under the authenticated player and WSOP scope. Read task 5 from the
retail lobby requested board 8, user 0, first rank 1 and 25 rows. The existing
range/around-player, individual-player and bulk-player storage queries support
this layout. Replies contain count, total entries and the four fields per row;
`0x1fabd8` reads total before decoding each row. Ranking uses descending signed
score with stable user-ID tie breaking.

The menu board-ID table at `0x4d1a78` contains `1,3,2,4,5,10,9,12,13,8,17,18`;
board 11 is also used by the offline/end-of-game writer. Automated tests cover
score writes (including zero and negative scores), ordering, total counts,
individual ranks, malformed bulk queries and isolation from WWE. Existing
unhandled submissions were not stored or backfilled. Retail menu readback and
fresh end-of-game submissions have since succeeded (see below).


The first retail Upload Offline Stats action sent four separate score-only
writes under the logged-in account, to boards 1, 2, 3 and 4, each with score 0.
The server accepted all four and PostgreSQL readback confirmed the four stored
rows with no extra columns. `DemonWareWriteOfflineStats` (`0x224870`) reads local
game-state counters and submits those four boards in sequence; it does not send
a saved-game blob. Before upload, the leaderboard menu successfully requested
both player-centered and top-rank reads for board 1, which was then empty.


## Retail validation

Two stock PCSX2 Sockets clients completed a direct game after the keepalive fix.
Both lobby connections remained alive, the host deleted its table and fresh
signed scores were stored in PostgreSQL. Offline-stat upload submitted boards
1–4, and a subsequent leaderboard read returned stored data.

## Completed forced-relay game

The shared SDK transport was temporarily switched from adaptive to relay mode,
with the WWE-only observer disabled for that test. Both PCSX2 clients needed a
fresh boot to discard their cached direct discovery result. Fresh discovery
assigned gameplay relay endpoints on server UDP 20001 and 20003. The clients
joined and completed a game without game or emulator patches.

The capture contains 426 laptop-to-desktop and 443 desktop-to-laptop gameplay
packets across about 101 seconds. Every captured gameplay payload matched its
forwarded counterpart byte-for-byte, and there were zero direct PC-to-PC UDP
3658 packets after relayed gameplay began. The host deleted the table and wrote
board 11 score 0 plus boards 5, 8, 17 and 18 score 1000; PostgreSQL readback
confirmed those values. Neither gateway closed during the test, and the joiner
sent another keepalive after the game ended.

After verification, the original adaptive transport and WWE observer settings
were restored and the temporary capture stopped. This establishes WSOP gameplay
compatibility with the shared opaque relay. Automatic fallback from a failed
direct path, physical consoles and EyeToy/video were not exercised by this test.
