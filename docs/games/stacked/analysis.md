# Stacked with Daniel Negreanu

Initial adapter for US PS2 `SLUS-212.59`, version 1.01. Retail signup, shared
account login, statistics initialization/readback, cash-table hosting/search and
two-player cash play over direct P2P work. A folded hand followed by an all-in
finished normally in the follow-up test. Forced-relay joining, gameplay and
post-game statistics writes also passed a subsequent two-player retail test.
Any/Quick Game and native filtered cash queries have protocol tests; controlled
restrictive-filter matches remain pending. Friend requests, acceptance and
game-invite joins also work in retail testing.
Persistent player profiles and multi-table tournament creation are not implemented yet.

## Disc and endpoints

Main ELF SHA-256:
`77a0c1891a56a65833ceb2f65dc3901f934853c4fb3b77dd2ae474be74aaa971`.
Addresses below refer to this executable. Private extraction and decompilation
remain in ignored `work/stacked/`.

| Setting | Value |
| --- | --- |
| Game key | `stacked` |
| Title ID | 302 (`0x12e`), initializer `0x1f1f18` |
| Auth | `auth.stacked.mmp.demonware.net`, TCP 3074 |
| Lobby | `lsg.stacked.mmp.demonware.net`, TCP 3074 |
| Retail discovery | `stacked.stun.us.demonware.net`, UDP 3074/3075 |
| Additional linked SDK names | `stun.{eu,us,jp,au}.demonware.net` |

The title-specific discovery name is at `0x5b1a10` and is required before login.
The initial retail attempt failed at this DNS lookup, before sending any TCP
login or UDP discovery packets. Redirecting only auth/lobby and the generic SDK
names is insufficient for this build.

## Shared protocol and differences

The connection sender at `0x4c7138` uses legacy u16 framing, 3DES-CBC and
`0xDEADBEEF`, matching WWE and OutRun. Task factory `0x278738` uses typed
slot/sequence/task bytes. Account IDs at `0x274ae8` hash the original name with
Tiger-192; signup `0x271e38`, login `0x272300` and ticket decoder `0x272b18`
use the existing credential and ticket format. Credentials are shared across
games; rooms, friends and statistics remain scoped to Stacked.

The combined **Create User / Login** button first sends account login, even for
a new username. `0x1f3c48` maps `BD_AUTH_BAD_ACCOUNT` (704) to UI state `0x24`;
`0x1ffe68` then calls `0x1f4428` to submit account creation. Returning generic
bad-request status 701 stops that flow with an error. The shared auth service
now returns 704 for a missing account, while duplicate signup still returns 707.

Unlike the other adapters, the service result parser uses an I32 error code
(`0x277fb0`), with strict marker checking at `0x26aec8`. The application selects
signed task status explicitly; other games retain U32 status fields.

IP discovery `0x268f50`/`0x268f68` uses padded v1. Traversal constructor
`0x2692a0` and serializer `0x269300` use packed v2 introductions with a ten-byte
authenticator, ID and two six-byte endpoints. The shared SDK relay worked
without a game-specific packet rewriter in a forced-relay retail test. Both
clients obtained virtual endpoints and proved their gameplay mappings; the
server forwarded 783 packets / 27,219 bytes in one direction and 701 packets /
25,889 bytes in the other over roughly 203 seconds. The capture contains no
direct UDP 4070-to-4070 gameplay flow. The user reported a normal finish; the
host deleted the room and both players successfully wrote boards 4, 5, 3, 14,
20 and 1. No server warning/error or unexpected gateway disconnect accompanied
the test. This verifies forced relay, not adaptive fallback after a failed
direct connection. The local test configuration was restored to adaptive mode.

Stacked can set `stun.share_transport_with` to the WWE SDK listener on the same
bind/public IP and UDP 3074/3075. Its v1 discovery and the other SDK games' v2
discovery feed the same traversal/relay router, with replies in the original
format. If OutRun's native discovery is also enabled, its identical v1 packets
use this common real-address policy; configure direct or adaptive mode, not
always-relay discovery. OutRun's native gameplay relay remains separate on
UDP 3658. The complete single-IP example covers this composition. Automated
checks cover actual mixed-version UDP relay, restrictive-NAT adaptive fallback,
configuration ordering, title routing and preservation of opaque gameplay.
The local combined service started with WWE, Family Feud, WSOP and Stacked on
one SDK listener, OutRun's native UDP service on 3658, and BRO on 2273/2274.
Live probes verified both discovery formats on 3074 and 3075, including switching
formats on the same source socket, and real-address/adaptive NAT replies. All
five local auth DNS names resolve to the shared server IP. With the combined
listener running, two freshly booted Stacked clients signed in, found the cash
table, joined and completed a game. Traffic used direct UDP 4070 in both
directions; the SDK introduction ended with zero relay offers. Both table
registrations were removed and both accounts saved post-game boards 4, 5, 3,
14, 20 and 1, without server warnings/errors or gateway disconnects. A subsequent OutRun
regression on the same running service completed both authenticated logins and
native relay registrations, created a room, returned it to the second player's
search, and reached gameplay with ongoing two-way native UDP 3658 relay traffic.
No server warning/error accompanied the OutRun startup/join check.

## Bootstrap and matchmaking

`0x1f3e68` submits service 8 task 1 with context zero and an empty query
(`0x277d80`), then consumes its status with a null result pointer. It does not
persist a player profile. Task 2 supports account existence checks for friend
proposals, as described below; other service 8 operations remain unsupported.

Table serializer `0x20f598` and reader `0x20fa60` use 36 fields:

- Three blobs: CommonAddr[33], room ID[8], room key[16].
- Four I32 slot fields: available seats, zero, maximum seats, zero. The native
  GameInfo counts at `+0x28`/`+0x29` are copied by `0x20f340`; the retail host
  advertises `7,0,8,0` after its initial zero-slot creation record.
- Twenty-five game values written as U32 by hosts and read as I32 in server
  responses (`0x210598`). The adapter converts response copies without changing
  stored room records or query types.
- Three I32 game values.
- A NUL-terminated name with a 32-byte buffer (31 content bytes).

CommonAddr `0x26ddf8` contains three padded local addresses, one padded public
address and a NAT byte. The shared matchmaking service takes the address length
as an adapter option rather than assuming every title uses 25 bytes.

The adapter acknowledges create/update/delete and returns rooms by ID using the
shared owner-scoped service. Query `0x1f8150`/`0x1f8190` is kind 1, a maximum
result count and I32 zero; it returns open public tables in creation order.
Retail **Quick Game** uses kind 7: I32 kind, I32 maximum (35), U32 flag and U32
zero. Constructors/serializer `0x1f82e8`/`0x1f8348` and the builder at `0x1f5bb0`
map that flag to room field 10. The adapter matches it exactly and returns only
tables with available seats. The other linked filtered classes use these room
fields in query order; all criteria must match exactly:

| Query kind | Room fields after kind/maximum |
| --- | --- |
| 2 | 10, 31 |
| 3 | 10, 12 |
| 4 | 10, 7 |
| 5 | 10, 7, 31 |
| 6 | 10, 12, 31 |
| 7 | 10 |

Every filtered class writes U32 criteria followed by U32 zero. Unsupported
classes, malformed markers and nonzero unmapped suffixes return no matches.
Native builders define the mappings; restrictive retail filter combinations
still need controlled positive/negative checks with a hosted table.
Matchmaking result counts use I32 markers (`0x275430`), selected through the
adapter's reply codec; the other games retain their existing U32 counts.

The first retail search returned one room server-side, but the client displayed
none because the copied host values still had U32 markers. Running the actual
`0x20fa60` room reader and `0x210598`/`0x26aec8` marker readers under Unicorn
confirmed that unsigned fields leave the game record uninitialized, while
signed fields populate it, including its name and game type. The private harness
stubs raw bit I/O, logging and common-address construction; executable code is
read from the user's extracted ELF and is not distributed. Protocol tests check
signed responses, multiple rooms, preserved bit values and unchanged stored data.

## Local retail check

Boot the ISO without patches, use PCSX2 Sockets with the local AngelWare DNS,
then sign in and create a table. Retail checks covered Quick Game discovery,
friend requests/acceptance, game-invite joins, direct play, a folded hand followed
by an all-in finish, and forced-relay play through a normal finish. Repeated
sign-ins retrieved the stored statistics records. Controlled restrictive-filter
matches and an adaptive fallback under failed direct connectivity remain
untested. Shared protocol regression tests alone do not establish those cases.

The first two-player hand ended with both players all-in and one player losing
all chips. Both clients remained connected, with steady direct UDP 4070 traffic,
but the joiner displayed a waiting-for-players message. `0x1ca4d8` selects this
message when its next-hand eligible-player count is below two; `0x1df0a0(2)`
displays `DIALOG_MTT_WAIT_PLAYER`. The same path is used for ordinary cash tables
despite the localization key's MTT name. This fits a bust-out/eligibility wait;
ordinary non-busting hands must still verify continuity before treating it as
either normal completion or a synchronization fault. In the follow-up invited
game, the user folded a hand, then went all-in and reported a proper finish.
Both clients submitted successful post-game writes for boards 8, 7, 9, 6, 16,
20 (bankroll) and 1. The host deleted its table; the other client briefly created
and then deleted another table, consistent with a host transition. No gateway
disconnect or server error accompanied this finish. The earlier all-in wait
was not reproduced in this test.

## Older statistics service

The first retail login requested service 4 task 3 for boards 20 and 19.
`0x212330` starts those two reads; a missing row makes `0x216370` call the game's
initialization writer `0x217be8`. For board 20, that writer initializes the local
bankroll to 2000 before submitting the record. The server returns an empty result
for a missing record and stores the game's resulting write.

This SDK uses task 1 for writes, task 3 for one-user reads and task 4 for range
reads (`0x279028`, `0x278ea8`, `0x278ed8`). Writes contain context U8, board I32,
rating I64, then variable columns serialized at `0x215da8` as String/U32/U64.
`INT64_MAX` means the rating is unchanged. The adapter preserves the previous
rating when present and stores typed columns in the game-owned stats `Extra`
record. Existing shared storage and account scoping remain unchanged.

Replies at `0x279158` contain an I32 count, with no separate total. Each result
is U64 user ID, I64 rating, then the stored columns; numeric columns use I32/I64
markers when read by `0x215f50`. This format does not add the newer SDK's implicit
rank/name fields. Other statistics task shapes remain unsupported.

## MTT and mailbox startup

After statistics, the retail client requests service 33 task 6 with its U64
account ID and no context byte. `0x45bef8`/`0x45fa38` submit a subscription
lookup; `0x45cdb8`, case 6, consumes a **U32** count. The adapter returns zero
subscriptions for the caller because tournament creation is not implemented.
Stacked Masters searches use task 3 (`0x45bd68`), with two U64 fields followed
by four U32 fields and no context byte. Both the default query and a changed
filter were observed in retail captures. The response parser at `0x45cdb8`,
case 3, also consumes a U32 count; these searches return the same empty
tournament catalogue. Other MTT operations fail explicitly; this does not
implement a multi-table tournament scheduler or claim creation succeeds.

The service 6 task 1 startup poll uses context U8 and Bool (`0x277988`) and
expects an **I32** result count (`0x277b30`). Task 4 acknowledges an I64 message ID, scoped to the caller. Notifications
remain queued until acknowledgement or resolution of their underlying request.

## Friends and invitations

Service 8 task 2 checks the target account before a proposal (`0x1eabc8`);
success has no result object, while status 101 selects user-not-found. Service 9
uses older task numbers: 1 propose, 2 answer proposal (U8 accept), 3 remove,
4 list, 5 block (U8 level), 7 invite (Blob8 session), 8 answer invitation.
The adapter maps these onto shared social persistence and duplicate filtering,
while preserving the wire task ID in replies. List results are I32 count then
U64 user, String name and I32 presence (`0x1ed1a0`).

Mailbox messages differ substantially from the later SDK: I32 type, U64 message
ID, I64 timestamp, two Strings, U64 sender, String sender name. Friend request,
acceptance and rejection use types 1/2/3; session invitations use type 6 and add
Blob8 session ID (`0x276308`, `0x2764a8`, `0x276850`, `0x277358`). The real
friend/session decoder routines accept the proposed payloads under Unicorn.
Unsupported presence and session-answer notifications are omitted because this
factory has no decoders for them.

The game fetches its inbox upon opening Friends (`0x1eb410`) and approximately
every 180 seconds in the background (`0x1e9b28`). New requests/invitations enqueue
notification graphics `FInvRec.tga`/`GInvRec.tga` (`0x1eca10`). This adapter uses
mailbox polling rather than unsolicited newer-SDK pushes. Retail testing confirmed the request appeared in Friends, was accepted and
acknowledged, and disappeared from subsequent inbox fetches. A game invitation
was then delivered, accepted and acknowledged, and the invited player joined
the hosted table and started a game. The capture shows two-way direct UDP 4070
traffic; this does not establish forced-relay gameplay or background popup timing.
