package frame

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"testing"
)

func TestMMP3RetailClientIntegrity(t *testing.T) {
	// SLUS-216.86 0x338860 executed under an R5900 interpreter with ring-buffer
	// I/O and the crypto primitives mocked. Input 05 070100000000, key 00..17,
	// seed 0x123456a5. The retail code chooses HMAC offsets and padding itself.
	plain, _ := hex.DecodeString("764442a405070100000000a5a5a5a5a5")
	key := make([]byte, 24)
	for i := range key {
		key[i] = byte(i)
	}
	typ, payload, err := MMP3.OpenClient(plain, key)
	if err != nil || typ != 5 || !bytes.Equal(payload, plain[5:]) {
		t.Fatalf("retail frame: %d %x %v", typ, payload, err)
	}
	for _, offset := range []int{0, 5, 15} {
		bad := bytes.Clone(plain)
		bad[offset] ^= 1
		if _, _, err := MMP3.OpenClient(bad, key); err == nil {
			t.Fatalf("accepted tampered byte %d", offset)
		}
	}
	if _, _, err := Open(plain); err == nil {
		t.Fatal("legacy decoder accepted MMP3")
	}
}

func TestFrameDialectsAndBounds(t *testing.T) {
	for _, p := range []Protocol{Legacy, MMP3} {
		wire := p.Plain(10, []byte{1, 2, 3})
		f, err := p.Read(bufio.NewReader(bytes.NewReader(wire)))
		if err != nil || f.Protocol != p || f.Type != 10 || !bytes.Equal(f.Payload, []byte{1, 2, 3}) {
			t.Fatalf("protocol %d: %+v %v", p, f, err)
		}
	}
	for _, wire := range [][]byte{
		{0xff, 0xff, 0xff, 0x7f}, // reject oversized declarations before allocation
		{1, 0, 0, 0},
		{2, 0, 0, 0, 2, 10},    // invalid encryption flag
		{2, 0, 0, 0, 1, 10},    // missing seed
		{8, 0, 0, 0, 0, 10, 1}, // truncated record
	} {
		if _, err := MMP3.Read(bufio.NewReader(bytes.NewReader(wire))); err == nil {
			t.Fatalf("accepted malformed frame %x", wire)
		}
	}
}

func TestMMP3KeepalivePreservesFollowingFrame(t *testing.T) {
	// SDK 1.80's 40-second keepalive is exactly a raw zero u32, with no body.
	wire := append([]byte{0, 0, 0, 0}, MMP3.Plain(1, []byte{7, 8})...)
	r := bufio.NewReader(bytes.NewReader(wire))
	alive, err := MMP3.Read(r)
	if err != nil || !alive.KeepAlive || alive.Protocol != MMP3 {
		t.Fatalf("keepalive: %+v %v", alive, err)
	}
	next, err := MMP3.Read(r)
	if err != nil || next.KeepAlive || next.Type != 1 || !bytes.Equal(next.Payload, []byte{7, 8}) {
		t.Fatalf("following frame: %+v %v", next, err)
	}
	if _, err := Legacy.Read(bufio.NewReader(bytes.NewReader([]byte{0, 0}))); err == nil {
		t.Fatal("changed legacy zero-length validation")
	}
}
