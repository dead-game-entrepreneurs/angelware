<p align="center">
  <img src="docs/assets/angelware-logo.png" alt="AngelWare" width="520">
</p>

<h3 align="center">Bringing PS2 DemonWare games back online</h3>

<p align="center">
  A replacement for <b>DemonWare</b>, the online backend behind a number of PS2 games,<br>
  so they can play online again with an unmodified disc.
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white">
  <img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql&logoColor=white">
  <img alt="Docker Compose" src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white">
  <img alt="PlayStation 2" src="https://img.shields.io/badge/PlayStation%202-online-003791?logo=playstation&logoColor=white">
</p>

<p align="center">
  <a href="#dns-redirects">DNS redirects</a> ·
  <a href="#pcsx2-outrun-2006">PCSX2 / OutRun</a> ·
  <a href="#running-the-server">Running the server</a> ·
  <a href="docs/games/svr2007/analysis.md">Protocol notes</a>
</p>

---

## Status

Supported games:

- **WWE SmackDown vs. Raw 2007** (US, `SLUS-214.27`, v1.01): the full online service.
- **OutRun 2006: Coast 2 Coast** (US PS2, `SLUS-212.74`): experimental online service,
  with an optional PNACH relay for PCSX2 Sockets. See [PCSX2 connection options](#pcsx2-outrun-2006).
- **Family Feud** (US PS2, `SLUS-214.46`, v1.02): guest login, matchmaking and two-player
  online games over direct P2P or the DemonWare relay. No PNACH required.
- **World Series of Poker 2008: Battle for the Bracelets** (US PS2, `SLUS-216.86`):
  experimental accounts, matchmaking, friends and leaderboards. Two unmodified PCSX2
  Sockets clients completed games over direct P2P and forced relay. No PNACH required.
  See [protocol notes](docs/games/wsop2008/analysis.md).
- **Call of Duty 2: Big Red One** (US): NAT detection and connecting players, with an optional relay
  for players whose routers won't connect directly. Its lobby and matchmaking run on GameSpy
  (OpenSpy), not DemonWare, so angelware only replaces the DemonWare STUN part.

### WWE SmackDown vs. Raw 2007

| | |
| --- | --- |
| Account sign-up and login | ✅ |
| Online lobby, hosting and finding matches | ✅ |
| Joining and playing a match (peer to peer) | ✅ |
| Player profiles and match records | ✅ |
| Stats written after a match | ✅ stored |
| Friends, friend requests, session invites | ✅ |
| Notifications kept for offline players (delivered at next sign-in) | ✅ |
| Friend messages | ✅ (sent directly between online players by the game) |
| Leaderboards and records | ✅ |
| Ranked matchmaking and search filters | 🧪 implemented, not yet tested in game |
| Trades (created superstar exchange) | ✅ |
| Private sessions (hidden from search, joined by invite) | ✅ |
| Change password, delete account, name checks | implemented, but the game never uses them |

The retail game has been tested in PCSX2: two players signed up, found each other's session and played a
match. Protocol notes are in [docs/games/svr2007/analysis.md](docs/games/svr2007/analysis.md).

### Family Feud

Online play connects two players, each controlling a family. The game signs in automatically
with a temporary guest identity; it does not use a username or password. Hosting, searching,
joining and completed games work in PCSX2 Sockets mode over direct P2P and the SDK relay.
No game or emulator patches are needed. Broader network coverage and physical-console testing
remain open. See [protocol notes](docs/games/familyfeud/analysis.md).

Set the game's DNS to your AngelWare resolver, including the generic SDK names listed below.
If hardware rendering only shows half the picture, try PCSX2's Software renderer for this game.

## How it fits together

```
 PS2 / PCSX2 ──DNS──▶ your DNS resolver ──▶ points the game's DemonWare names at angelware
      │
      ├── DNAS check ──────────▶ community DNAS replacement (not part of angelware)
      ├── TCP 3074  ───────────▶ angelware: sign-up, login, lobby
      ├── UDP 3074/3075 ───────▶ angelware: NAT detection (WWE, OutRun and Family Feud can share one IP)
      └── UDP 2273/2274 ───────▶ angelware: NAT detection and connecting players (CoD BRO)
                                     └── UDP 20000-20199: DemonWare relay endpoints
                                     │
                                 PostgreSQL: accounts, profiles, stats
```

angelware does **not** include a DNS server. You run your own and add the records below.
OutRun's native room probes and race traffic use peer UDP 3658. Its current direct mode does
not route those packets through WWE's adaptive relay. The optional PNACH transport uses
OutRun's own authenticated relay on server UDP 3658; it requires the patch on every client.

## DNS redirects

Point these names at the IP address of the machine running angelware (an `A` record or a DNS override):

| Hostname | Used for |
| --- | --- |
| `smackdownvsraw2007.us.ps2.auth.mmp.demonware.net` | Sign-up and login |
| `smackdownvsraw2007.us.ps2.lsg.mmp.demonware.net` | Lobby (matchmaking, profiles, stats) |
| `smackdownvsraw2007.stun.us.demonware.net` | NAT detection, connecting players |
| `smackdownvsraw2007.stun.eu.demonware.net` | NAT detection (the game asks for both) |

**Shared SDK discovery — Family Feud and WSOP 2008:**

Both games use these during startup, before title-specific login. Redirect all
four names along with the game's own names below.

| Hostname | Used for |
| --- | --- |
| `stun.us.demonware.net` | SDK discovery (US) |
| `stun.eu.demonware.net` | SDK discovery (Europe) |
| `stun.jp.demonware.net` | SDK discovery (Japan) |
| `stun.au.demonware.net` | SDK discovery (Australia) |

For **OutRun 2006: Coast 2 Coast**:

| Hostname | Used for |
| --- | --- |
| `auth.outrun2.mmp.demonware.net` | Sign-up and login |
| `lsg.outrun2.mmp.demonware.net` | Lobby and matchmaking |
| `outrun2.stun.us.demonware.net` | IP and NAT discovery |
| `outrun2.stun.eu.demonware.net` | IP and NAT discovery |

For **Family Feud**:

| Hostname | Used for |
| --- | --- |
| `familyfeud.auth.mmp.demonware.net` | Guest login |
| `familyfeud.lsg.mmp.demonware.net` | Lobby and matchmaking |
| `familyfeud.stun.us.demonware.net` | IP/NAT discovery and peer introductions |
| `familyfeud.stun.eu.demonware.net` | IP/NAT discovery and peer introductions |

For **World Series of Poker 2008: Battle for the Bracelets**:

| Hostname | Used for |
| --- | --- |
| `wsop2008-ps2-auth.mmp3.demonware.net` | Sign-up and login |
| `wsop2008-ps2-lsg.mmp3.demonware.net` | Lobby and matchmaking |

See [WSOP setup](#pcsx2-world-series-of-poker-2008) for account and invitation details.

For **Call of Duty 2: Big Red One**:

| Hostname | Used for |
| --- | --- |
| `bro.stun.activision.com` | NAT detection, connecting players |

BRO's GameSpy names go to OpenSpy as usual.

**DNAS:** the game checks DNAS before going online. angelware does not handle this. Forward
`*.dnas.playstation.org` lookups (the game asks for `gate1.us.dnas.playstation.org`) to the community
DNAS replacement's resolver at **`45.7.228.197`**.

Everything else can resolve normally.

<details>
<summary>Example: dnsmasq</summary>

Replace `203.0.113.10` with your angelware server's IP.

```
address=/smackdownvsraw2007.us.ps2.auth.mmp.demonware.net/203.0.113.10
address=/smackdownvsraw2007.us.ps2.lsg.mmp.demonware.net/203.0.113.10
address=/smackdownvsraw2007.stun.us.demonware.net/203.0.113.10
address=/smackdownvsraw2007.stun.eu.demonware.net/203.0.113.10
address=/auth.outrun2.mmp.demonware.net/203.0.113.10
address=/lsg.outrun2.mmp.demonware.net/203.0.113.10
address=/outrun2.stun.us.demonware.net/203.0.113.10
address=/outrun2.stun.eu.demonware.net/203.0.113.10
address=/wsop2008-ps2-auth.mmp3.demonware.net/203.0.113.10
address=/wsop2008-ps2-lsg.mmp3.demonware.net/203.0.113.10
address=/familyfeud.auth.mmp.demonware.net/203.0.113.10
address=/familyfeud.lsg.mmp.demonware.net/203.0.113.10
address=/familyfeud.stun.us.demonware.net/203.0.113.10
address=/familyfeud.stun.eu.demonware.net/203.0.113.10
address=/stun.us.demonware.net/203.0.113.10
address=/stun.eu.demonware.net/203.0.113.10
address=/stun.jp.demonware.net/203.0.113.10
address=/stun.au.demonware.net/203.0.113.10
address=/bro.stun.activision.com/203.0.113.10
server=/dnas.playstation.org/45.7.228.197
```

</details>

Then set the DNS server in the game's network configuration (or in PCSX2's network settings) to your
resolver.

## PCSX2: OutRun 2006

Configure the game's network profile to use your AngelWare DNS resolver, then choose one of
these connection options. The PNACH below targets **US `SLUS-21274`, CRC `38C16493`**.

### Sockets with the PNACH relay (recommended)

This supports automatic hosting through AngelWare, including PCSX2 Flatpak.

1. In **Settings → Network & HDD**, enable Ethernet, select **Sockets**, and choose your adapter.
2. Copy [SLUS-21274_38C16493.pnach](patches/outrun2006/SLUS-21274_38C16493.pnach) into PCSX2's
   **patches** folder. Find the active folder under **Settings → Folders**. Flatpak normally uses
   `~/.var/app/net.pcsx2.PCSX2/config/PCSX2/patches/`.
3. Open **OutRun → Properties → Patches**, click **Reload**, and enable **AngelWare native relay**.
4. Fully stop and boot the game, then log in and create or join a room. Use a fresh boot when
   enabling or updating the patch, rather than loading an old save state.

**Both players need the PNACH for a relayed session.** The server must enable OutRun's
`options.native_relay` and allow UDP **3658**. See the [relay setup](patches/outrun2006/README.md)
for server configuration. The disc image and emulator binary stay unchanged.

### PCAP (direct connection)

Use **PCAP Bridged** with your network adapter and configure a valid LAN address, gateway,
and AngelWare DNS in the game's network profile. Disable the relay PNACH for direct play.
The emulated consoles need direct UDP **3658** connectivity, including any necessary firewall
or router forwarding.

Linux PCAP requires raw-packet permissions (`CAP_NET_RAW` and `CAP_NET_ADMIN`). Flatpak's
sandbox does not support this path; use Sockets there. A “no network cable” error can mean
PCSX2 could not open the adapter. See the [PCSX2 PCAP backend notes](https://github.com/PCSX2/pcsx2/blob/master/pcsx2/DEV9/pcap_io.cpp).

### Sockets without the patch: host / back-out workaround

With the relay PNACH disabled on both clients, this sequence can prime the direct UDP sockets:

1. **Player B** creates a temporary room.
2. **Player A** tries **Quick Game** once, then backs out and immediately creates the actual room.
   Continue even if the search reports no sessions.
3. **Player B** leaves the temporary room and uses **Quick Game** to join Player A.

Complete the switch within about **two minutes**, before the socket mappings expire. This
still needs working direct connectivity and may need repeating after an emulator restart or
an idle timeout. Use the PNACH relay when direct connections cannot get through the network.

## PCSX2: World Series of Poker 2008

Use the US disc (`SLUS-216.86`) with PCSX2 Sockets and the DNS redirects above.
No PNACH is required. Accounts, unfiltered table browsing, friends, invitations,
score-based leaderboards and offline-stat uploads are supported. Restrictive
search filters remain unsupported. Use a lowercase username when sharing an
account with older games.

To accept a game invite, open Friends and select the inviting friend. Reopen
that friend's options if they were already selected when the invite arrived;
the shipped PS2 menu has no general invite popup.

After a server restart, fully stop and boot both games without loading save
states to refresh cached network discovery before testing joins.

## Running the server

You need Docker with Compose.

```sh
git clone https://github.com/princebyyourlogic/angelware.git
cd angelware
cp .env.example .env
chmod 600 .env
```

Edit `.env`: set `ANGELWARE_PUBLIC_IP` to the address players reach this machine on (your LAN IP for a
home test, the public IP on a VPS) and choose a `POSTGRES_PASSWORD`. Then:

```sh
docker compose up -d --build
docker compose logs -f angelware
```

Open these in your firewall: **TCP 3074**, **UDP 3074-3075**, and for CoD BRO **UDP 2273-2274** and
the relay range **UDP 20000-20199**. Enable **UDP 3658** for OutRun's optional native relay.

angelware runs with host networking so it sees each player's real address, which NAT detection needs.
PostgreSQL is only reachable from the machine itself. Stop with `docker compose down`; add `-v` only if
you want to delete all accounts and stats.

### Multiple games

The original environment variables continue to start WWE and optional CoD BRO. To select games
and give each its own network addresses, copy `configs/angelware.example.json` to
`configs/angelware.json`, replace the example IPs, and set
`ANGELWARE_CONFIG=/etc/angelware/angelware.json` in `.env`. Compose mounts `configs/` at that path.
Outside Docker, set `ANGELWARE_CONFIG` to the local file path. The database URL can remain in
`ANGELWARE_DATABASE_URL`; it does not need to appear in the JSON file.

The JSON file selects enabled games. Each entry supplies `game`, `bind`, `public_ip`, optional
`tcp_port`, `stun`, and game-specific `options`. Unknown game names and options fail startup.
Defaults come from the game's definition. Empty `games` is an error.

WWE and OutRun can share one IPv4 and UDP 3074/3075 with the explicit configuration in
[`configs/single-ip.example.json`](configs/single-ip.example.json). Set OutRun's
`stun.share_with` to `svr2007` and give both entries the same `bind` and `public_ip`. The listener
routes OutRun's padded v1 discovery into its own direct router and WWE's packed v2 into its
existing router. Their peer state, NAT policy and relay capacity remain separate. OutRun's
optional native gameplay relay still uses UDP 3658; this does not enable adaptive OutRun gameplay.

Family Feud and WSOP 2008 can join WWE's packed-v2 SDK transport with
`"stun": { "share_transport_with": "svr2007" }` and matching bind/public addresses.
This shares discovery, traversal, relay capacity and the owner's routing policy. Auth, rooms and
lobby handlers remain separate; WWE's peer hook only rewrites packets bound to WWE room keys.
Family Feud uses temporary guest identities rather than password accounts. Completed games have
passed on both direct and forced-relay paths. See [protocol notes](docs/games/familyfeud/analysis.md).

Sharing aliases cannot chain or override the owner's listener settings. Titles requiring different
SDK routing policies need separate addresses/listeners. Ambiguous binds fail startup.
TCP auth/lobby routes by title ID and ticket. CoD BRO uses UDP 2273/2274 on the same IP.
Behind NAT, forward the selected ports to the configured server bind address.

Games that use account names and passwords share those credentials. Rooms, profiles, friends, mailbox, statistics,
and uploaded files remain separate. Existing gameplay records migrate into `svr2007`; existing
credentials become unified accounts. Numbered migrations run transactionally under
a database lock. Take a database backup before upgrading an existing installation; older binaries
cannot use the new schema.

Game packages and the extension workflow are described in [the architecture guide](docs/architecture.md).
See [deployment and rollback](docs/deployment.md) before upgrading a hosted installation.

### Settings

All set in `.env`:

| Variable | Default | What it does |
| --- | --- | --- |
| `ANGELWARE_PUBLIC_IP` | required | IPv4 address players reach the server on |
| `POSTGRES_PASSWORD` | required | Database password |
| `ANGELWARE_BIND` | `0.0.0.0` | Address to listen on |
| `ANGELWARE_PORT` | `3074` | Main port, TCP and UDP (the game expects 3074) |
| `ANGELWARE_STUN_ALT_PORT` | `3075` | Second UDP port for NAT detection |
| `ANGELWARE_STUN_MODE` | `direct` | SvR 2007 connection mode; keep `direct` (see below) |
| `ANGELWARE_SVR_RELAY_REWRITE` | `off` | Experimental SvR match-address rewrite on relayed connections; see below |
| `ANGELWARE_SVR_OBSERVER` | `off` | Observe authenticated stock-host peer-count reports; requires adaptive + rewrite |
| `ANGELWARE_BRO` | `on` | `off` disables the CoD BRO service |
| `ANGELWARE_BRO_PORT`, `ANGELWARE_BRO_ALT_PORT` | `2273`, `2274` | CoD BRO's NAT detection ports |
| `ANGELWARE_BRO_MODE` | `relay` | CoD BRO connection mode (see below) |
| `ANGELWARE_RELAY_PORTS` | `20000-20199` | UDP ports for relay endpoints, one per player |
| `ANGELWARE_RELAY_MAX_CLIENTS` | `64` | Players a title's relay serves at once |
| `ANGELWARE_DIRECT_WINDOW` | `1.0` | Seconds `adaptive` lets a direct connection try before offering the relay |
| `ANGELWARE_PG_PORT` | `55433` | Local-only port PostgreSQL is published on |
| `ANGELWARE_AUTH_MODE` | `verify` | `verify` checks passwords. `open` lets anyone log in as any name |
| `ANGELWARE_LOG_LEVEL` | `info` | `debug` logs every lobby request |

### Connection modes

Each game's NAT service runs in one of three modes, all on a single public IPv4 address:

- **`direct`**: players connect to each other directly; angelware only introduces them. Players
  behind strict NATs may fail to connect. This is how SvR 2007 has been tested.
- **`relay`**: every player is given a relay endpoint on this server and all peer traffic flows
  through it. The game sees every player as Open NAT, so anyone can play with anyone, at the cost of
  server bandwidth and an extra hop.
- **`adaptive`**: players keep their real addresses and try to connect directly first. If a direct
  connection hasn't worked within `ANGELWARE_DIRECT_WINDOW`, the relay is offered instead.

All players must sign in again and create new rooms after the mode changes. The relay and adaptive
modes are a port of codstun, written for CoD BRO.

**SvR 2007 defaults to `direct`; relay mode needs the experimental rewrite below.** Its matches don't run over DemonWare. The game's own netcode
connects to the host's public address, or to the host's LAN address when both players share a public
IP. Every relay endpoint shares the server's IP, so in `relay` mode every pair takes the LAN branch
and the match fails. The host sends the address the joiner uses over an encrypted DemonWare peer
channel, which the ordinary relay forwards unchanged. Rewriting only the matchmaking result does
not work: the host replaces that result after joining.

An experimental fix is available with `ANGELWARE_SVR_RELAY_REWRITE=on` and `ANGELWARE_STUN_MODE=relay`
or `adaptive`. The server records login randomness, reconstructs a peer key against the actual
handshake public key, and rewrites both host-address copies in the encrypted join-accept to the
host's relay endpoint. It then re-encrypts the packet and recomputes its HMAC. Only connections
already admitted to a relay pair are changed; direct connections retain their addresses.
Captured-packet tests pass for distinct and shared public IPs. Live two-client tests reached
controllable gameplay through both adaptive branches: blocked direct UDP triggered relay fallback
and verified game DATA/ACK cycles through the relay; a fresh unblocked room used direct game
DATA/ACKs with no relay-port game traffic. Both players must sign in again and create a new room
after enabling it. See
[peer encryption and relay rewriting](docs/games/svr2007/peer-encryption.md).

`ANGELWARE_SVR_OBSERVER=on` observes the stock host's authenticated matchmaking updates.
A fresh two-player room changing from fewer than two native peers to two can confirm prematch
connection completion when exactly one guest gateway received its room key and that guest's IP
identifies one registered non-host UDP endpoint. The observer checks room generation, ownership,
and relay admission before recording a direct result. A current direct result suppresses extra
relay offers only for that same outstanding signed traversal. Multiple key recipients, ambiguous
UDP registrations, or missing reports remain unmeasured and keep native-retry fallback intact.
It adds no peer, opens no extra UDP port, and does not alter room capacity. This measures native
prematch completion, not the later GSS address's reachability.

Without `ANGELWARE_SVR_RELAY_REWRITE=on`, `adaptive` can help the lobby join, but the match still goes direct.

To test two emulators behind one router, set PCSX2's network adapter to a bridged mode (PCAP or TAP)
so each emulated PS2 gets a real LAN address. In the default sockets mode every PS2 reports the same
internal address (192.0.2.100), so the game's LAN connection goes nowhere.

### Accounts and privacy

For hosted beta testing, [continuous packet capture and incident bundles](docs/beta-diagnostics.md)
provide private, rotated PCAPs, debug logs, and task/reply timing using
`compose.beta.yaml`. The default capture window is up to 72 hours within 8 GiB.

Players create their account from the game's online menu. The game never sends a plain password; it sends
a hash, which the protocol then uses as the login key. That hash is stored in the `password_tiger` column
and works like a password for this game, so keep the database and its backups private. angelware stores
no email addresses or real names.

## Development

No local Go install is needed; the toolchain runs in Docker:

```sh
scripts/go test ./...
scripts/go vet ./...
scripts/test-postgres  # disposable database: migrations and isolation checks
```

PostgreSQL tests in `internal/storage` run when `ANGELWARE_TEST_DATABASE_URL` points at a database.

| Path | Contents |
| --- | --- |
| `cmd/angelware` | Explicit game registration and process entry point |
| `internal/app` | Configuration, runtime composition and listeners |
| `internal/game` | Game factory contracts |
| `internal/protocol/dw` | Auth, gateway, tickets, bit buffers, framing and crypto |
| `internal/services` | Shared room lifecycle, stats, social, mailbox and file services |
| `internal/games` | Game-specific codecs, search policies, settings and peer behavior |
| `internal/connectivity/stun` | Discovery, traversal and relay |
| `internal/storage` | Game-scoped PostgreSQL and memory storage, numbered migrations |
| `tools/` | Disc extraction helpers for reverse engineering. They need your own disc and are not used by the server |

### Game files

This repository contains no game code, disc images or assets. The reverse-engineering tools read files
from your own disc into `disc/` and `work/`, which are ignored by git.
Please keep it that way in contributions.
