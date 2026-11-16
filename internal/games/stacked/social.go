package stacked

import (
	"errors"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// SDK senders 0x274010..0x2745c0 use combined answer operations and put
// LoadFriends at task 4. Replies must still echo the original wire task.
func Friends() services.Handler {
	return services.FriendsWithOptions(services.FriendsOptions{
		Decode: decodeFriends,
		EncodeListReply: func(t services.Task, count uint32, fields []bitbuf.Field) []byte {
			// Game result 0x1ed1a0 reads U64 user, String name, I32 presence.
			for i := range fields {
				if fields[i].Type == bitbuf.Bool {
					fields[i].Type = bitbuf.I32
				}
			}
			return signedListReply(t, count, fields)
		},
	})
}

func decodeFriends(task uint8, f []bitbuf.Field) (uint8, []bitbuf.Field, error) {
	bad := errors.New("invalid older SDK friends request")
	if len(f) < 1 || f[0].Type != bitbuf.U8 || f[0].Int != 0 {
		return 0, nil, bad
	}
	if task == 4 && len(f) == 1 {
		return services.FriendList, nil, nil
	}
	if len(f) < 2 || f[1].Type != bitbuf.U64 || f[1].Int == 0 {
		return 0, nil, bad
	}
	args := f[1:]
	switch task {
	case 1, 3:
		if len(f) == 2 {
			if task == 1 {
				return services.FriendPropose, args, nil
			}
			return services.FriendRemove, args, nil
		}
	case 2, 5, 8:
		if len(f) != 3 || f[2].Type != bitbuf.U8 || f[2].Int > 1 {
			break
		}
		if task == 5 {
			return services.FriendBlock, args, nil
		}
		if task == 2 {
			if f[2].Int == 1 {
				return services.FriendAccept, args[:1], nil
			}
			return services.FriendReject, args[:1], nil
		}
		if f[2].Int == 1 {
			return services.FriendInviteAccept, args[:1], nil
		}
		return services.FriendInviteReject, args[:1], nil
	case 7:
		if len(f) == 3 && f[2].Type == bitbuf.Blob && len(f[2].Bytes) == 8 {
			return services.FriendInvite, args, nil
		}
	}
	return 0, nil, bad
}

// Mailbox factory 0x2776c0 reads an I32 type, then the header (0x276308),
// two strings (0x2764a8), sender U64/name (0x276850), and for wire type 6
// the native session blob (0x277358). It has no presence/answer factories.
func MessageFields(m store.Message) []bitbuf.Field {
	typ := m.Type
	switch typ {
	case 1, 2, 3:
	case 5:
		if len(m.Data) != 8 {
			return nil
		}
		typ = 6
	default:
		return nil
	}
	f := []bitbuf.Field{
		bitbuf.Int(bitbuf.I32, uint64(typ)),
		bitbuf.Int(bitbuf.U64, m.ID),
		bitbuf.Int(bitbuf.I64, uint64(m.Created.Unix())),
		bitbuf.Bytes(bitbuf.String, nil), bitbuf.Bytes(bitbuf.String, nil),
		bitbuf.Int(bitbuf.U64, m.From),
		bitbuf.Bytes(bitbuf.String, []byte(m.FromName)),
	}
	if typ == 6 {
		f = append(f, bitbuf.Bytes(bitbuf.Blob, m.Data))
	}
	return f
}
