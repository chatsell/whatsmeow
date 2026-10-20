// Chatsell patch, not upstream: WhatsApp scheduled messages.
//
// A scheduled message is delivered when it is scheduled, as a
// ConditionalRevealMessage whose body is AES-256-GCM encrypted (12-byte IV, no
// AAD) with a random 32-byte reveal key. How the key reaches a device (from
// WhatsApp Web's own modules: WAWebHandleMsgParser, WAWebScheduledMsgCrypto,
// WAWebScheduledMsg{Post,Reveal}NotificationHandler):
//
//   - The sender's own devices get it inline, in the same <message>:
//     <meta type="scheduled_message" st="<unix s>"><key rkid="<uuid>">32 bytes
//     (or 64 hex chars)</key></meta>. At the scheduled time the server sends a
//     mex NotificationScheduledMessagePost {rkid, status}; SUCCESS means it
//     went out.
//   - Recipients get it at the scheduled time in a mex
//     NotificationScheduledMessageReveal {chat_jid, rk (url-safe base64),
//     rkid, st}. The key can arrive before the message.

package whatsmeow

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	scheduledMessageMetaType = "scheduled_message"
	scheduledRevealKeyBytes  = 32

	mexScheduledMessagePost   = "NotificationScheduledMessagePost"
	mexScheduledMessageReveal = "NotificationScheduledMessageReveal"
)

// parseScheduledMessageMeta reads the scheduling data of a <meta> child of a
// <message>. It returns nil when the meta is about something else.
func parseScheduledMessageMeta(meta *waBinary.Node) (*types.ScheduledMessageMeta, error) {
	ag := meta.AttrGetter()
	if ag.OptionalString("type") != scheduledMessageMetaType {
		return nil, nil
	}
	scheduled := &types.ScheduledMessageMeta{ScheduledTimestamp: ag.OptionalUnixTime("st")}
	if keyNode, ok := meta.GetOptionalChildByTag("key"); ok {
		scheduled.RevealKeyID = keyNode.AttrGetter().OptionalString("rkid")
		if content, ok := keyNode.Content.([]byte); ok {
			scheduled.RevealKey = normalizeRevealKey(content)
		}
	}
	return scheduled, ag.Error()
}

// normalizeRevealKey accepts the raw 32 bytes or their 64-char hex form, as
// WhatsApp Web does; anything else is dropped.
func normalizeRevealKey(content []byte) []byte {
	switch len(content) {
	case scheduledRevealKeyBytes:
		return content
	case scheduledRevealKeyBytes * 2:
		if decoded, err := hex.DecodeString(string(content)); err == nil && len(decoded) == scheduledRevealKeyBytes {
			return decoded
		}
	}
	return nil
}

// flexibleUnix accepts a unix timestamp sent as a JSON number or string.
type flexibleUnix int64

func (f *flexibleUnix) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseInt(strings.Trim(string(data), `"`), 10, 64)
	if err != nil {
		return err
	}
	*f = flexibleUnix(value)
	return nil
}

type scheduledMexWrapper struct {
	Data struct {
		Post *struct {
			RevealKeyID string `json:"rkid"`
			Status      string `json:"status"`
		} `json:"xwa2_notify_scheduled_message_post"`
		Reveal *struct {
			ChatJID     string       `json:"chat_jid"`
			RevealKey   string       `json:"rk"`
			RevealKeyID string       `json:"rkid"`
			Scheduled   flexibleUnix `json:"st"`
		} `json:"xwa2_notify_scheduled_message_reveal"`
	} `json:"data"`
}

// handleScheduledMessageMex dispatches the two scheduled message mex
// notifications. It reports whether data was one of them.
func (cli *Client) handleScheduledMessageMex(mnd events.MexNotificationData, data []byte) bool {
	if mnd.OpName != mexScheduledMessagePost && mnd.OpName != mexScheduledMessageReveal {
		return false
	}
	var wrapper scheduledMexWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		cli.Log.Warnf("Failed to parse %s mex notification: %v", mnd.OpName, err)
		return true
	}
	switch {
	case wrapper.Data.Post != nil:
		cli.Log.Infof("Scheduled message post notification for %s: %s", wrapper.Data.Post.RevealKeyID, wrapper.Data.Post.Status)
		cli.dispatchEvent(&events.ScheduledMessagePost{
			Mex:         mnd,
			RevealKeyID: wrapper.Data.Post.RevealKeyID,
			Status:      wrapper.Data.Post.Status,
		})
	case wrapper.Data.Reveal != nil:
		reveal := wrapper.Data.Reveal
		chat, err := types.ParseJID(reveal.ChatJID)
		if err != nil {
			cli.Log.Warnf("Invalid chat_jid %q in scheduled message reveal %s: %v", reveal.ChatJID, reveal.RevealKeyID, err)
		}
		key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(reveal.RevealKey, "="))
		if err != nil || len(key) != scheduledRevealKeyBytes {
			cli.Log.Warnf("Invalid reveal key in scheduled message reveal %s (%d bytes, err: %v)", reveal.RevealKeyID, len(key), err)
			key = nil
		}
		cli.Log.Infof("Scheduled message reveal notification for %s in %s", reveal.RevealKeyID, reveal.ChatJID)
		var scheduledAt time.Time
		if reveal.Scheduled > 0 {
			scheduledAt = time.Unix(int64(reveal.Scheduled), 0)
		}
		cli.dispatchEvent(&events.ScheduledMessageReveal{
			Mex:                mnd,
			Chat:               chat,
			RevealKeyID:        reveal.RevealKeyID,
			RevealKey:          key,
			ScheduledTimestamp: scheduledAt,
		})
	default:
		cli.Log.Warnf("%s mex notification without known data: %s", mnd.OpName, revealKeyJSON.ReplaceAll(data, []byte(`"rk":"[redacted]"`)))
	}
	return true
}
