package svr2007

import (
	"encoding/binary"
	"net/netip"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
)

func nativePeers(info []bitbuf.Field) (int, bool) {
	if len(info) < 7 {
		return 0, false
	}
	for i := 3; i < 7; i++ {
		if info[i].Type != bitbuf.I32 || info[i].Int > 2 {
			return 0, false
		}
	}
	if info[3].Int+info[5].Int != 2 || info[4].Int > info[3].Int || info[6].Int > info[5].Int {
		return 0, false
	}
	return int(info[4].Int + info[6].Int), true
}

// InspectConnectivity decodes the stock WWE two-player room report.
func InspectConnectivity(info []bitbuf.Field) (netip.AddrPort, int, bool) {
	n, ok := nativePeers(info)
	if !ok || info[0].Type != bitbuf.Blob || len(info[0].Bytes) != 25 {
		return netip.AddrPort{}, 0, false
	}
	a := info[0].Bytes[18:24]
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte(a[:4])), binary.LittleEndian.Uint16(a[4:])), n, true
}
