package svr2007

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// Profiles, lobby service 8 (LBYM 0x6ce08..0x6d240, lobby tasks 0x0e..0x14):
//
//	1 create(u8, i32, i32, blob[48])  2 read public(u8, u64 user)  3 read private(u8)
//	4 update public (as create)       5 set private(u8, 4 x i32)  6 delete(u8)
//	7 exists(u8, u64 user) -> bool
//
// Public result (0x26960): u64 user, i32, i32, blob. Private result (0x26b18):
// u64 user, 4 x i32: TotalDisconnect, TitleDisconnect, TitleSingleDisconnect,
// TitleTagDisconnect. The game sets the first to 1 before a match and clears it
// after a clean finish, so quitters keep it.
const (
	profCreate      = 1
	profReadPublic  = 2
	profReadPrivate = 3
	profUpdate      = 4
	profSetPrivate  = 5
	profDelete      = 6
	profExists      = 7

	publicDataSize = 48
)

func Profiles(ctx context.Context, c *services.Conn, t services.Task) []byte {
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 1 {
		c.Log().Warn("profile task did not parse", "task", t.ID, "err", err)
		return services.Reply(t, nil)
	}
	args := fields[1:]
	user := c.UserID()
	st := c.Store()

	switch t.ID {
	case profCreate, profUpdate:
		if len(args) >= 3 && args[2].Type == bitbuf.Blob {
			p := PublicProfile{A: int32(args[0].Int), B: int32(args[1].Int), Data: args[2].Bytes}
			if err := setProfile(ctx, st, user, "public", p); err != nil {
				c.Log().Error("save public profile", "err", err)
			}
		}
		return services.Reply(t, nil)
	case profReadPublic:
		if len(args) < 1 {
			return services.Reply(t, nil)
		}
		target := args[0].Int
		p, err := publicProfile(ctx, st, target)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			c.Log().Error("load public profile", "err", err)
		}
		if p.Data == nil {
			p.Data = make([]byte, publicDataSize)
		}
		return services.Reply(t, []bitbuf.Field{bitbuf.Int(bitbuf.U64, target),
			bitbuf.Int(bitbuf.I32, uint64(uint32(p.A))), bitbuf.Int(bitbuf.I32, uint64(uint32(p.B))),
			bitbuf.Bytes(bitbuf.Blob, p.Data)})
	case profReadPrivate:
		p, err := privateProfile(ctx, st, user)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			c.Log().Error("load private profile", "err", err)
		}
		out := []bitbuf.Field{bitbuf.Int(bitbuf.U64, user)}
		for _, v := range p {
			out = append(out, bitbuf.Int(bitbuf.I32, uint64(uint32(v))))
		}
		return services.Reply(t, out)
	case profSetPrivate:
		var p PrivateProfile
		n := 0
		for _, f := range args {
			if f.Type == bitbuf.I32 && n < len(p) {
				p[n] = int32(f.Int)
				n++
			}
		}
		if n == len(p) {
			if err := setProfile(ctx, st, user, "private", p); err != nil {
				c.Log().Error("save private profile", "err", err)
			}
			c.Log().Info("private profile saved", "values", p)
		}
		return services.Reply(t, nil)
	case profDelete:
		if err := st.DeleteProfile(ctx, user); err != nil {
			c.Log().Error("delete profile", "err", err)
		}
		return services.Reply(t, nil)
	case profExists:
		exists := false
		if len(args) > 0 {
			_, err := publicProfile(ctx, st, args[0].Int)
			exists = err == nil
		}
		var b uint64
		if exists {
			b = 1
		}
		return services.Reply(t, []bitbuf.Field{bitbuf.Int(bitbuf.Bool, b)})
	}
	c.Log().Info("profile task not implemented", "task", t.ID)
	return services.Reply(t, nil)
}

type PublicProfile struct {
	A, B int32
	Data []byte
}
type PrivateProfile [4]int32

func setProfile(ctx context.Context, st store.Store, user uint64, kind string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return st.SetProfile(ctx, user, kind, store.Profile{Version: 1, Payload: payload})
}
func readProfile(ctx context.Context, st store.Store, user uint64, kind string, value any) error {
	r, err := st.Profile(ctx, user, kind)
	if err != nil {
		return err
	}
	if r.Version != 1 {
		return errors.New("unsupported WWE profile version")
	}
	return json.Unmarshal(r.Payload, value)
}
func publicProfile(ctx context.Context, st store.Store, user uint64) (PublicProfile, error) {
	var p PublicProfile
	err := readProfile(ctx, st, user, "public", &p)
	return p, err
}
func privateProfile(ctx context.Context, st store.Store, user uint64) (PrivateProfile, error) {
	var p PrivateProfile
	err := readProfile(ctx, st, user, "private", &p)
	return p, err
}
