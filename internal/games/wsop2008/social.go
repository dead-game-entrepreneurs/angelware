package wsop2008

import (
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// MessageFields follows WSOP's registered notification factories. Invitation 5
// uses 0x348610 (sender, session blob, optional data). Answers 6/7 and presence
// 9/10 use 0x349ac8, which ends after the sender name. Trailing legacy fields
// would misalign the following notification in a mailbox fetch.
func MessageFields(m store.Message) []bitbuf.Field {
	f := services.DefaultMessageFields(m)
	switch m.Type {
	case 6, 7, 9, 10:
		return f[:7]
	default:
		return f
	}
}
