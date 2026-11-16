# AngelWare game architecture

AngelWare composes one runtime per enabled game. Credentials are shared across the service. DemonWare transport and
reusable services are shared code; each runtime has its own handlers, gameplay storage scope, rooms, online players, and
connectivity policy. Compatible titles can explicitly share SDK transport state. The executable
registers factories explicitly in `cmd/angelware/main.go`.

## Package boundaries

| Package | Responsibility |
| --- | --- |
| `internal/app` | Load configuration, build runtimes, validate and own listeners |
| `internal/game` | Definition, factory options and runtime contracts |
| `internal/protocol/dw` | Framing, typed fields, encryption, auth, tickets and gateway lifecycle |
| `internal/services` | Room ownership and lifecycle, stats operations, friends, mailbox and files |
| `internal/games/<key>` | Game codecs, query interpretation, service composition and settings |
| `internal/connectivity/stun` | UDP discovery, peer introduction and relay transport |
| `internal/storage` | Bound game stores, PostgreSQL migrations and memory implementation |

Shared packages never import a concrete game. Each game's `New` function registers its service
handlers once; gateway requests dispatch through that registry. A handler may switch on a
protocol task ID, but shared handlers do not switch on game identity. Unsupported services fail
explicitly. CoD BRO composes connectivity without auth or lobby services. A game definition with
no STUN port may compose TCP services alone.

Matchmaking owns room IDs, keys, connection ownership, lifecycle and disclosure tracking.
Game policies interpret session fields and search criteria. Optional connectivity inspection
and completion callbacks keep WWE's two-player observer rules in its own package. WWE's peer
rewriter also lives there. On a shared SDK transport it checks its own room/key binding before
changing traffic; another title's associations remain opaque.

The TCP listener selects the legacy or MMP3 wire dialect using registered auth
IDs or issued lobby tickets. Tickets bind both game and dialect. Framing,
integrity checks, keepalives and task correlation belong to the protocol layer;
game adapters select their dialect without adding game-name checks there.
Notification codecs are selected per runtime and used consistently for pushes
and mailbox fetches.

Stats services own reads, writes, paging and deletion. Game codecs decode writes and encode rows,
including wire integer types, extra columns and board ordering. Storage preserves integer
columns and optional opaque extra data, such as OutRun's 64-bit ghost-file ID. Profile payloads
are versioned records interpreted by the game, rather than a universal fixed-size structure.

## Identity and isolation

The internal game key identifies an isolation scope. Numeric title IDs identify auth requests;
a definition can register more than one wire ID for variants sharing a runtime. Registration
on the same TCP listener rejects duplicate wire IDs.

Auth selects a registered title before touching its store. Login tickets bind the game key and
actual wire title ID to a random server-side session token. A gateway redeems that token and
opens the corresponding game application; it does not accept a game identity from lobby tasks.
A ticket for a game unavailable on that listener is rejected.

Anonymous SDK request `0x10` is enabled only by titles that opt into guest login. It issues a
fresh random temporary identity and a game-scoped lobby ticket, without writing an account.
The session records its guest status; neither source IP nor request seed identifies an account.
Password titles reject this request. Family Feud registers only matchmaking for these guests.

`Database.ForGame(key)` combines the shared `Accounts` interface with permanently scoped gameplay
storage. A username has one user ID and password across supported games. Gameplay SQL filters,
conflicts and mutations include the game scope; account-name joins use the shared directory.
Memory stores share a credential directory with its own lock. Room, presence and invite maps
belong to individual runtime instances. File and mailbox lookups and mutations require the game
scope. Password changes affect the unified account. Authenticated account deletion removes the
identity and its owned data in every game; profile deletion affects only that game.

Migration 0001 recognizes the original schema. Migration 0002 assigns existing records to
`svr2007`, changes composite keys and converts WWE profiles to version 1 JSON records. Migration 0003 moves credentials into the shared directory. It refuses conflicting per-game
passwords instead of silently choosing one. Each migration and its version record commit in one
transaction, serialized by an advisory lock.

## Network configuration

`ANGELWARE_CONFIG` selects a JSON file; the original environment configuration remains supported
when it is absent. Game settings are decoded by that game's factory with unknown fields rejected.
Database credentials may remain in `ANGELWARE_DATABASE_URL`.

TCP listeners group compatible games by bind address and port. Auth routes by title ID; lobby
routes by ticket. UDP discovery packets carry no auth title ID. A definition declares its SDK
discovery version and whether it uses an independent native peer transport. Such a game can
set `stun.share_with` to another enabled listener. Bind/public addresses must match,
aliases cannot chain, and aliases cannot override the owner's socket settings. DNS names
are not a UDP routing key.

Compatible SDK titles may instead set `stun.share_transport_with`. Supported discovery
versions can differ: both use packed-v2 traversal. Definitions must use the same discovery
ports. Members inherit one owner's complete SDK router, relay capacity and adaptive policy.
Bind/public addresses must match; native-transport members, setting overrides, cycles and
alias chains are rejected. Discovery replies retain each request's wire format.

A native-only discovery version gets an independent direct router without an SDK relay
allocator or peer hook. Traversal follows the endpoint learned during discovery; a new
explicit discovery updates ownership. If a native title and an SDK member use the same
discovery version, their packets are indistinguishable. That version instead uses the common
SDK router under direct or adaptive policy. Always-relay mode is rejected because its
virtual advertised addresses would break native transport. These overlapping clients share
peer state, admission limits and relay capacity; native gameplay still uses its own service.
Thus OutRun remains separate when sharing only with v2 SDK games, and joins the common
real-address discovery policy when v1 Stacked is also enabled. No title is guessed from IP,
source port, uninitialized padding or DNS history. TCP authentication identifies the
account/title but supplies no UDP endpoint; discovery can precede login, and multiple
clients behind NAT share the TCP source IP. A client MAC is not visible across routed
networks. Room records can associate a host endpoint later, but do not identify the
initial discovery socket or every searcher's UDP endpoint.

Application room lookup and lobby tickets remain game-scoped, and encrypted peer protocols
authenticate their own sessions. Game-owned rewrite hooks must verify their own room/key
bindings before modifying anything; their registry is frozen before serving packets. WWE's
direct-completion observer retains its authenticated room-generation checks and declines
ambiguous endpoint matches. Shared UDP logs identify the transport rather than guessing a
title. Independent per-title NAT/relay policies require separate listeners.
A runtime may also provide `game.PacketService` instances for its native peer transport; the
application validates their UDP binds, starts them, and cancels/joins them with the other services.
Startup rejects overlapping discovery/native sockets and conflicting TCP wildcard binds. Relay ranges
may be shared: each runtime binds free ports and keeps its own admission and routing state.

## Adding a game

1. Create `internal/games/<key>` with a `Definition` and `New` factory.
2. Record supported builds, title IDs, endpoints and verified protocol layouts under `docs/games/<key>`.
3. Register compatible shared services and implement the game's differing codecs and policies.
4. Keep peer processing in the game package until independent evidence establishes a shared format.
5. Register the definition in `cmd/angelware/main.go` and add an entry to the runtime configuration.
6. Add protocol fixtures and tests for that build. Extend a shared interface only when a concrete
   game needs it, and add an isolation regression for any newly shared state.

Validation commands are `scripts/go test ./...`, `scripts/go vet ./...`, and
`scripts/test-postgres`. CI runs the race detector and PostgreSQL integration tests. Protocol tests
exercise console-style encrypted requests, but retail emulator gameplay remains a separate check.
