package services

import (
	"bytes"
	"context"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// Friends, lobby service 9 (senders LBYM 0x60a90..0x61920, lobby tasks 0x15..0x21):
//
//	1  ProposeFriend(u8, u64 user[, blob, u32])        -> message 1 to them
//	   (ignored between friends; repeats are not re-sent; if they already asked
//	   us, it accepts their request instead)
//	13 WithdrawProposalFriend(u8, u64 user)             -> message 4
//	2  AcceptProposeFriend(u8, u64 user)                -> message 2
//	3  RejectProposeFriend(u8, u64 user)                -> message 3
//	4  RemoveFriend(u8, u64 user)
//	19 LoadFriendProposeList(u8)  -> u64 user, string name
//	5  LoadFriendList(u8)         -> u64 user, string name, bool online
//	6  SetBlockUser(u8, u64 user, u8 level)
//	7  LoadBlockUserList(u8)      -> u64 user, string name, u8 level
//	12 SendFriendMessage(u8, u64 user, ..., blob text, ...)        -> message 8
//	8  InviteGameSession(u8, u64 user, blob session id[8])         -> message 5
//	9  AcceptInvitation(u8, u64 user) / 10 RejectInvitation        -> message 6 / 7
//
// Captured from the retail game: ProposeFriend carried only u8 + u64, and an invite
// u8 + u64 + the hosted session's 8-byte id (which the invitee then looks up with
// matchmaking task 4).
//
// Friendships, requests and blocks persist; every notification goes through the
// mailbox so a player who is offline sees it at their next login.
const (
	FriendPropose      = 1
	FriendAccept       = 2
	FriendReject       = 3
	FriendRemove       = 4
	FriendList         = 5
	FriendBlock        = 6
	FriendBlocks       = 7
	FriendInvite       = 8
	FriendInviteAccept = 9
	FriendInviteReject = 10
	FriendMessage      = 12
	FriendWithdraw     = 13
	FriendRequests     = 19
)

// invite remembers the session of a sent invite so the answer can carry it back
// to the inviter.
type invite struct {
	session  []byte
	answered bool
}

// firstBlob returns the first blob among a task's arguments.
func firstBlob(args []bitbuf.Field) []byte {
	for _, f := range args {
		if f.Type == bitbuf.Blob {
			return f.Bytes
		}
	}
	return nil
}

// FriendsOptions adapts SDK operation numbers and list codecs while keeping the
// shared persistence, duplicate filtering and invitation lifecycle.
type FriendsOptions struct {
	Decode          func(task uint8, fields []bitbuf.Field) (operation uint8, args []bitbuf.Field, err error)
	EncodeListReply func(Task, uint32, []bitbuf.Field) []byte
}

func FriendsWithOptions(o FriendsOptions) Handler {
	return func(ctx context.Context, c *Conn, t Task) []byte { return c.friends(ctx, t, o) }
}

func (c *Conn) friends(ctx context.Context, t Task, o FriendsOptions) []byte {
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 1 {
		c.log.Warn("friends task did not parse", "task", t.ID, "err", err)
		if o.Decode != nil {
			return ErrorReply(t, 1)
		}
		return Reply(t, nil)
	}
	args := fields[1:]
	operation := t.ID
	if o.Decode != nil {
		operation, args, err = o.Decode(t.ID, fields)
		if err != nil {
			return ErrorReply(t, 1)
		}
	}
	listReply := ListReply
	if o.EncodeListReply != nil {
		listReply = o.EncodeListReply
	}
	st, me := c.srv.Store, c.sess.UserID

	// Tasks that name another player carry their id first.
	var other uint64
	if len(args) > 0 && args[0].Type == bitbuf.U64 {
		other = args[0].Int
	}
	note := func(to uint64, typ uint32) store.Message {
		return store.Message{To: to, Type: typ, From: me, FromName: c.sess.Name}
	}
	fail := func(what string, err error) {
		if err != nil {
			c.log.Error(what, "err", err)
		}
	}
	clear := func(from, to uint64, typ uint32) {
		fail("resolve pending notification", st.ClearMessages(ctx, to, from, typ))
	}

	switch operation {
	case FriendPropose:
		if other == 0 || other == me {
			return Reply(t, nil)
		}
		if blocked, _ := st.IsBlocked(ctx, other, me); blocked {
			c.log.Info("friend request to a player who blocked us dropped")
			return Reply(t, nil)
		}
		if already, err := st.AreFriends(ctx, me, other); already || err != nil {
			fail("check friendship", err)
			c.log.Info("friend request between existing friends ignored")
			if already {
				clear(me, other, msgFriendRequest)
				clear(other, me, msgFriendRequest)
			}
			return Reply(t, nil)
		}
		// They already asked us: treat our request as accepting theirs.
		if crossed, err := st.RemoveFriendRequest(ctx, other, me); crossed {
			clear(other, me, msgFriendRequest)
			fail("save friendship", st.AddFriendship(ctx, me, other))
			c.srv.Notify(ctx, note(other, msgFriendAccepted))
			c.log.Info("crossed friend requests: now friends")
			return Reply(t, nil)
		} else {
			fail("check reverse request", err)
		}
		m := note(other, msgFriendRequest)
		m.Data = firstBlob(args)
		created, err := st.AddFriendRequest(ctx, me, other, m.Data)
		fail("save friend request", err)
		if created {
			c.srv.Notify(ctx, m)
		} else {
			c.log.Info("repeated friend request not sent again")
		}
	case FriendWithdraw:
		clear(me, other, msgFriendRequest)
		if ok, err := st.RemoveFriendRequest(ctx, me, other); ok {
			c.srv.Notify(ctx, note(other, msgFriendCanceled))
		} else {
			fail("withdraw friend request", err)
		}
	case FriendAccept:
		clear(other, me, msgFriendRequest)
		ok, err := st.RemoveFriendRequest(ctx, other, me)
		fail("accept friend request", err)
		if ok {
			fail("save friendship", st.AddFriendship(ctx, me, other))
			c.srv.Notify(ctx, note(other, msgFriendAccepted))
		}
	case FriendReject:
		clear(other, me, msgFriendRequest)
		if ok, err := st.RemoveFriendRequest(ctx, other, me); ok {
			c.srv.Notify(ctx, note(other, msgFriendRejected))
		} else {
			fail("reject friend request", err)
		}
	case FriendRemove:
		fail("remove friend", st.RemoveFriendship(ctx, me, other))
	case FriendRequests:
		list, err := st.FriendRequestsTo(ctx, me)
		fail("load friend requests", err)
		var out []bitbuf.Field
		for _, f := range list {
			out = append(out, bitbuf.Int(bitbuf.U64, f.UserID), bitbuf.Bytes(bitbuf.String, []byte(f.Name)))
		}
		return listReply(t, uint32(len(list)), out)
	case FriendList:
		list, err := st.Friends(ctx, me)
		fail("load friends", err)
		var out []bitbuf.Field
		for _, f := range list {
			var online uint64
			if c.srv.Online(f.UserID) != nil {
				online = 1
			}
			out = append(out, bitbuf.Int(bitbuf.U64, f.UserID), bitbuf.Bytes(bitbuf.String, []byte(f.Name)),
				bitbuf.Int(bitbuf.Bool, online))
		}
		return listReply(t, uint32(len(list)), out)
	case FriendBlock:
		if len(args) >= 2 && other != 0 {
			fail("save block", st.SetBlock(ctx, me, other, uint8(args[1].Int)))
		}
	case FriendBlocks:
		list, err := st.Blocks(ctx, me)
		fail("load blocks", err)
		var out []bitbuf.Field
		for _, f := range list {
			out = append(out, bitbuf.Int(bitbuf.U64, f.UserID), bitbuf.Bytes(bitbuf.String, []byte(f.Name)),
				bitbuf.Int(bitbuf.U8, uint64(f.Level)))
		}
		return listReply(t, uint32(len(list)), out)
	case FriendMessage:
		if blocked, _ := st.IsBlocked(ctx, other, me); blocked || other == 0 {
			return Reply(t, nil)
		}
		m := note(other, msgFriendMessage)
		m.Data = firstBlob(args)
		c.srv.Notify(ctx, m)
	case FriendInvite:
		if blocked, err := st.IsBlocked(ctx, other, me); blocked || err != nil || other == 0 || other == me {
			return Reply(t, nil)
		}
		inv := invite{session: firstBlob(args)}
		if len(inv.session) != 8 || !c.srv.setInvite(me, other, inv) {
			return Reply(t, nil)
		}
		clear(me, other, msgInvite)
		m := note(other, msgInvite)
		m.Data = inv.session
		c.srv.Notify(ctx, m)
	case FriendInviteAccept, FriendInviteReject:
		clear(other, me, msgInvite)
		inv, pending := c.srv.takeInvite(other, me)
		if !pending {
			return Reply(t, nil)
		}
		typ := uint32(msgInviteAccepted)
		if operation == FriendInviteReject {
			typ = msgInviteRejected
		}
		m := note(other, typ)
		m.Data = inv.session
		c.srv.Notify(ctx, m)
	default:
		c.log.Info("friends task not implemented", "task", t.ID, "args", fields)
		if t.ID == 16 || t.ID == 17 {
			return listReply(t, 0, nil)
		}
	}
	return Reply(t, nil)
}

func (s *Server) setInvite(from, to uint64, inv invite) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invites == nil {
		s.invites = map[[2]uint64]invite{}
	}
	if previous, ok := s.invites[[2]uint64{from, to}]; ok && bytes.Equal(previous.session, inv.session) {
		return false
	}
	inv.session = bytes.Clone(inv.session)
	s.invites[[2]uint64{from, to}] = inv
	return true
}

func (s *Server) takeInvite(from, to uint64) (invite, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uint64{from, to}
	inv, ok := s.invites[key]
	if !ok || inv.answered {
		return invite{}, false
	}
	inv.answered = true
	s.invites[key] = inv
	return inv, true
}
