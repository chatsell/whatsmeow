// Chatsell patch, not upstream: events for WhatsApp scheduled messages. See
// whatsmeow/chatsell_scheduled.go for the protocol.

package events

import (
	"time"

	"go.mau.fi/whatsmeow/types"
)

// ScheduledMessagePost is emitted at the scheduled time of a message the
// account itself scheduled. Status "SUCCESS" means it went out.
type ScheduledMessagePost struct {
	Mex         MexNotificationData
	RevealKeyID string
	Status      string
}

// ScheduledMessageReveal carries the key of a message someone else scheduled
// for this account, at its scheduled time. RevealKey is nil if it was invalid.
type ScheduledMessageReveal struct {
	Mex                MexNotificationData
	Chat               types.JID
	RevealKeyID        string
	RevealKey          []byte
	ScheduledTimestamp time.Time
}
