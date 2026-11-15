package outrun2006

import (
	"context"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// This build uses service 8 as a query service, not WWE's profiles. During
// login it submits the default query with no serialized object fields and ignores its result object
// (0x12a530 -> 0x12afc0 -> 0x158e38, null result pointer).
func BootstrapQuery(_ context.Context, _ *services.Conn, t services.Task) []byte {
	f, err := t.Args.Fields()
	if err != nil || t.ID != 1 || len(f) != 1 || f[0].Type != bitbuf.U8 {
		return services.ErrorReply(t, 1)
	}
	return services.Reply(t, nil)
}
