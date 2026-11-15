# Call of Duty 2 Big Red One support

The `codbro` game definition composes DemonWare discovery, traversal and relay on UDP 2273/2274.
Its lobby and matchmaking remain on GameSpy/OpenSpy, so this runtime registers no TCP auth or
lobby handlers. The default connectivity mode is relay, with ports 20000 through 20199 used for
peer endpoints. Redirect `bro.stun.activision.com` to the configured reachable IP.
