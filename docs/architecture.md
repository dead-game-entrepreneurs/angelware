# AngelWare game architecture

AngelWare composes one runtime per enabled game. Credentials are shared across the service. DemonWare transport and
reusable services are shared code; each runtime has its own handlers, gameplay storage scope, rooms, online players, and
connectivity state. The executable registers factories explicitly in `cmd/angelware/main.go`.

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
rewriter also lives there; enabling another game never attaches that rewriter to its traffic.

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
routes by ticket. UDP discovery packets carry no auth title ID, so games with overlapping UDP
ports need separate bind IPs. DNS does not provide a second routing key after a datagram arrives.
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
