# OutRun Sockets relay patch (experimental)

This PNACH lets the US PS2 game open and keep its native UDP socket active, then route room
probes, joins and race packets through AngelWare. It is intended for stock PCSX2's Sockets
backend, including Flatpak. It changes the game in memory, not the ISO or emulator binary. An unmodified physical PS2
uses the original direct native transport; it does not load PCSX2 PNACH files or speak this
relay protocol. Adaptive routing for OutRun has not been implemented.

Supported disc: **SLUS-21274, CRC 38C16493**. Other regions/revisions need separate verified hooks.
The compiled shim and server protocol tests pass. Stock Flatpak PCSX2 has registered,
received keepalive replies and created a room directly with the virtual relay address.
A second stock Flatpak client has discovered and joined that room, and both clients
completed a relay race and saved their results. The desktop reported zero shim drops/read
errors. The tester noted a possible synchronization issue but deferred characterizing it;
play quality still needs broader tester feedback.

## Setup

1. On the server, set OutRun's `options.native_relay` to `true`. Keep its `stun.mode` as `direct`.
   Allow UDP **3658** to the OutRun server address as well as the existing login/discovery ports.
   Restart AngelWare. This option is off by default and independent of WWE's relay.
2. On **both** PCs, put [SLUS-21274_38C16493.pnach](SLUS-21274_38C16493.pnach) in PCSX2's
   **patches** folder. Use Settings → Folders to find the active data directory.
   Flatpak's default is `~/.var/app/net.pcsx2.PCSX2/config/PCSX2/patches/`.
3. Enable **AngelWare native relay** in this game's Properties → Patches. Set its Network & HDD
   Ethernet backend to **Sockets**, using the normal adapter and working AngelWare DNS.
4. Completely stop and boot the game on each PC. Do not load an old save state for this test.
   Both clients need the patch; patched and unpatched native connections are not mixed.

Restart PCSX2 or use the Patches page's Reload button if the entry is not visible after
copying the file. A file placed in `patches` is listed on the Patches page.

Revision 3 expands the game's per-thread socket staging capacity and matching allocation
from 2048 to 4096 bytes. The relay envelope plus a full native packet can then be sent
and received without exceeding the game API's request-size limit. Both allocation literals
are verified against the supported ELF. Revision 2 registered successfully at the server
but could not read ACKs because even a 32-byte ACK was rejected when the receive request
asked for 2081 bytes from a 2048-byte staging buffer.

Revision 2 introduced the central network pump (`0x21dd18`) to register and consume the
relay ACK while the player is still in the online menus. Revision 1 waited on a native
worker that is not yet scheduled before hosting, causing create-session timeout. Replace
any older revision on both PCs and fully stop/boot; reloading a changed code shim into a running
game is not a supported update path.

## Live test

Log in on both PCs. Create a room directly on the first PC, without searching first. On the
second, use Quick Game to discover and join it. Start and finish a race, leave, then swap hosts
and repeat. The server should log `native peer registered` once for each connection with distinct
`10.254.x.y` identities, followed by the normal room, search, and stats events. No native packet
should need to go directly from one player's LAN/public address to the other.

Also test: fresh host after a full emulator restart; host idle for more than two minutes before
a guest searches; leave/recreate; logout/login; same LAN; different networks. Check both saved
race results and rankings. More than two players and ghost transfers need separate validation.

To disable, uncheck the patch and reboot the game. Set `native_relay=false` and restart the
server to return to the original direct setup. Existing accounts and results are unaffected.

## Desktop diagnostics

With the game running and PCSX2's existing PINE option enabled, run:

```sh
python3 tools/outrun_patch_status.py
```

This first verifies the loaded code against the current PNACH. It then reports only server/
virtual addresses, registration/ACK counters, readiness, and socket buffer capacities. It
skips lobby tokens and native packet contents. After login, expect `ready: 1`, a `10.254.x.y`
virtual address, growing ACK counts and 4096-byte socket contexts. `--pid` selects one instance
if several emulators are running; `--socket` accepts an explicit PINE Unix-socket path.

## Rebuild and verification

Requires Python 3, clang, ld.lld, llvm-objcopy and llvm-nm. Build intermediates stay in ignored
`work/outrun2006/pnach-build`. Supplying the extracted retail ELF additionally checks its hash,
hook instructions, symbol-table bounds and zero padding before building:

```sh
python3 patches/outrun2006/build.py --elf work/outrun2006/ps2/SLUS_212.74
python3 patches/outrun2006/test_transport.py  # requires Python unicorn 2.1.4
ANGELWARE_GO_IMAGE=angelware:go-race scripts/go test -race -count=1 ./...
```

The source and PNACH contain no accounts, passwords, lobby tokens, or game executable. The
relay's protocol and limits are documented in [the game analysis](../../docs/games/outrun2006/analysis.md).
