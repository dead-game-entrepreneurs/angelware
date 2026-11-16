package stacked

import (
	"context"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// 0x275430 and 0x277b30 consume signed list counts; MTT's 0x45cdb8
// deliberately differs and consumes an unsigned count for subscriptions.
func signedListReply(t services.Task, count uint32, fields []bitbuf.Field) []byte {
	f := []bitbuf.Field{bitbuf.Int(bitbuf.I32, uint64(count))}
	return services.Reply(t, append(f, fields...))
}

// 0x45bd68 submits catalogue queries; 0x45bef8/0x460bf8 submit subscription
// lookups. The catalogue is empty until tournament creation is implemented.
// Both result branches at 0x45cdb8 consume unsigned counts.
func MTT(_ context.Context, c *services.Conn, t services.Task) []byte {
	f, err := t.Args.Fields()
	if err != nil {
		return services.ErrorReply(t, 1)
	}
	switch t.ID {
	case 3:
		// Both retail default and filtered Stacked Masters queries serialize
		// two U64 and four U32 fields, without a context byte.
		if len(f) != 6 {
			return services.ErrorReply(t, 1)
		}
		for i, field := range f {
			want := uint8(bitbuf.U32)
			if i < 2 {
				want = bitbuf.U64
			}
			if field.Type != want {
				return services.ErrorReply(t, 1)
			}
		}
		c.Log().Debug("MTT catalogue searched", "returned", 0)
	case 6:
		if len(f) != 1 || f[0].Type != bitbuf.U64 || f[0].Int != c.UserID() {
			return services.ErrorReply(t, 1)
		}
	default:
		return services.ErrorReply(t, 1)
	}
	return services.ListReply(t, 0, nil)
}

// 0x277988 polls with context U8 and a Bool, without offset/limit fields.
// Notifications stay queued until the game acknowledges them.
func MailboxPoll(ctx context.Context, c *services.Conn, t services.Task) []byte {
	f, err := t.Args.Fields()
	if err != nil || len(f) != 2 || f[0].Type != bitbuf.U8 || f[0].Int != 0 {
		return services.ErrorReply(t, 1)
	}
	switch t.ID {
	case 1:
		if f[1].Type != bitbuf.Bool {
			return services.ErrorReply(t, 1)
		}
		pending, err := c.Store().Inbox(ctx, c.UserID(), 0, 50)
		if err != nil {
			return services.ErrorReply(t, 1)
		}
		var out []bitbuf.Field
		var count uint32
		for _, m := range pending {
			if m.Type == 1 {
				requests, err := c.Store().FriendRequestsTo(ctx, c.UserID())
				if err != nil {
					return services.ErrorReply(t, 1)
				}
				found := false
				for _, r := range requests {
					if r.UserID == m.From {
						found = true
						break
					}
				}
				if !found {
					if err := c.Store().ClearMessages(ctx, c.UserID(), m.From, 1); err != nil {
						return services.ErrorReply(t, 1)
					}
					continue
				}
			}
			fields := MessageFields(m)
			if len(fields) == 0 {
				return services.ErrorReply(t, 1)
			}
			out = append(out, fields...)
			count++
		}
		c.Log().Debug("Stacked mailbox fetched", "messages", count)
		return signedListReply(t, count, out)
	case 4:
		if f[1].Type != bitbuf.I64 || c.Store().Ack(ctx, c.UserID(), f[1].Int) != nil {
			return services.ErrorReply(t, 1)
		}
		return services.Reply(t, nil)
	default:
		return services.ErrorReply(t, 1)
	}
}
