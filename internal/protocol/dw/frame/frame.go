// Package frame implements the bdStreamSocket framing used by the auth service and
// the lobby service gateway (LBYM send 0x896d8, receive 0x8a068):
//
//	u16 LE length | u8 encrypted | ...
//	plain:     u8 message type | payload           (length = payload + 2)
//	encrypted: u32 seed | 3DES-CBC ciphertext      (length = ciphertext + 5)
//
// MMP3 uses a u32 LE length instead, and client ciphertext starts with a four-byte
// HMAC tag. Legacy ciphertext and encrypted server replies start with Magic.
// Both clients accept plain frames from the server in every state.
package frame

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Magic starts legacy client bodies and encrypted server replies.
const Magic = 0xDEADBEEF

// Protocol selects a verified SDK wire dialect. Legacy is the default for MMP;
// MMP3 uses 32-bit lengths and authenticated client ciphertext.
type Protocol uint8

const (
	Legacy Protocol = iota
	MMP3
)

func (p Protocol) HeaderSize() int {
	if p == MMP3 {
		return 4
	}
	return 2
}

func (p Protocol) MaxLength() int {
	if p == MMP3 {
		return 0x3bf6 // SLUS-216.86 bdLobbyConnection receive limit
	}
	return 65535
}

type Frame struct {
	Protocol  Protocol
	KeepAlive bool // MMP3 zero-length stream record; no type or payload
	Encrypted bool
	Type      uint8  // valid when !Encrypted
	Seed      uint32 // valid when Encrypted
	Payload   []byte // message payload, or the ciphertext when Encrypted
}

// Read reads one frame.
func Read(r *bufio.Reader) (Frame, error) {
	return Legacy.Read(r)
}

func (p Protocol) Read(r *bufio.Reader) (Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:p.HeaderSize()]); err != nil {
		return Frame{}, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 && p == MMP3 {
		return Frame{Protocol: p, KeepAlive: true}, nil
	}
	if n < 2 || n > uint32(p.MaxLength()) {
		return Frame{}, fmt.Errorf("frame: invalid length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	if body[0] == 1 {
		if n < 5 {
			return Frame{}, errors.New("frame: encrypted frame without seed")
		}
		return Frame{Protocol: p, Encrypted: true, Seed: binary.LittleEndian.Uint32(body[1:]), Payload: body[5:]}, nil
	}
	if body[0] != 0 {
		return Frame{}, errors.New("frame: invalid encryption flag")
	}
	return Frame{Protocol: p, Type: body[1], Payload: body[2:]}, nil
}

// Plain encodes an unencrypted frame.
func Plain(msgType uint8, payload []byte) []byte {
	return Legacy.Plain(msgType, payload)
}

func (p Protocol) Plain(msgType uint8, payload []byte) []byte {
	h := p.HeaderSize()
	out := make([]byte, h+2, h+2+len(payload))
	if p == MMP3 {
		binary.LittleEndian.PutUint32(out, uint32(len(payload)+2))
	} else {
		binary.LittleEndian.PutUint16(out, uint16(len(payload)+2))
	}
	out[h+1] = msgType
	return append(out, payload...)
}

// OpenClient validates a decrypted client frame. MMP3 authenticates payload and
// padding with the first four bytes of HMAC-SHA1; its type byte is excluded.
// Server-to-client encrypted frames still use Magic in both dialects.
func (p Protocol) OpenClient(plain, key []byte) (uint8, []byte, error) {
	if p != MMP3 {
		return Open(plain)
	}
	if len(plain) < 8 || len(plain)%8 != 0 {
		return 0, nil, errors.New("frame: invalid MMP3 plaintext length")
	}
	mac := hmac.New(sha1.New, key)
	mac.Write(plain[5:])
	if !hmac.Equal(plain[:4], mac.Sum(nil)[:4]) {
		return 0, nil, errors.New("frame: invalid MMP3 authentication tag")
	}
	return plain[4], plain[5:], nil
}

// Open checks and splits a decrypted frame body into its type and payload.
func Open(plain []byte) (uint8, []byte, error) {
	if len(plain) < 5 || binary.LittleEndian.Uint32(plain) != Magic {
		return 0, nil, errors.New("frame: bad magic after decryption")
	}
	return plain[4], plain[5:], nil
}
