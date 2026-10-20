package whatsmeow

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var testRevealKey = bytes.Repeat([]byte{0xab}, 32)

func scheduledMeta(key []byte) waBinary.Node {
	return waBinary.Node{
		Tag:   "meta",
		Attrs: waBinary.Attrs{"type": "scheduled_message", "st": "1792494000"},
		Content: []waBinary.Node{{
			Tag:     "key",
			Attrs:   waBinary.Attrs{"rkid": "31200a70-4891-435d-841d-74a85ddc525f"},
			Content: key,
		}},
	}
}

func TestParseScheduledMessageMeta(t *testing.T) {
	for name, key := range map[string][]byte{
		"raw": testRevealKey,
		"hex": []byte(hex.EncodeToString(testRevealKey)),
	} {
		t.Run(name, func(t *testing.T) {
			node := scheduledMeta(key)
			meta, err := parseScheduledMessageMeta(&node)
			if err != nil || meta == nil {
				t.Fatalf("meta=%v err=%v", meta, err)
			}
			if meta.RevealKeyID != "31200a70-4891-435d-841d-74a85ddc525f" || !bytes.Equal(meta.RevealKey, testRevealKey) {
				t.Fatalf("unexpected meta %+v", meta)
			}
			if !meta.ScheduledTimestamp.Equal(time.Unix(1792494000, 0)) {
				t.Fatalf("scheduled at %v", meta.ScheduledTimestamp)
			}
		})
	}

	t.Run("wrong key length is dropped", func(t *testing.T) {
		node := scheduledMeta([]byte("short"))
		meta, _ := parseScheduledMessageMeta(&node)
		if meta == nil || meta.RevealKey != nil {
			t.Fatalf("unexpected meta %+v", meta)
		}
	})

	t.Run("other meta types are ignored", func(t *testing.T) {
		node := waBinary.Node{Tag: "meta", Attrs: waBinary.Attrs{"target_id": "X"}}
		if meta, err := parseScheduledMessageMeta(&node); meta != nil || err != nil {
			t.Fatalf("meta=%v err=%v", meta, err)
		}
	})
}

func TestLoggableNodeRedactsRevealKeys(t *testing.T) {
	message := waBinary.Node{Tag: "message", Content: []waBinary.Node{scheduledMeta(testRevealKey)}}
	logged := loggableNode(&message)
	if strings.Contains(logged, hex.EncodeToString(testRevealKey)) || !strings.Contains(logged, "redacted 32 bytes") {
		t.Fatalf("reveal key not redacted: %s", logged)
	}
	if !mentionsScheduling(&message, 3) {
		t.Fatal("type=scheduled_message must be detected by value")
	}

	mex := waBinary.Node{Tag: "update", Attrs: waBinary.Attrs{"op_name": mexScheduledMessageReveal},
		Content: []byte(`{"data":{"xwa2_notify_scheduled_message_reveal":{"rk":"c2VjcmV0","rkid":"x"}}}`)}
	if logged := loggableNode(&mex); strings.Contains(logged, "c2VjcmV0") {
		t.Fatalf("mex reveal key not redacted: %s", logged)
	}
}

func TestHandleScheduledMessageMex(t *testing.T) {
	cli := &Client{Log: waLog.Noop}
	var got []any
	cli.AddEventHandler(func(evt any) { got = append(got, evt) })

	post := []byte(`{"data":{"xwa2_notify_scheduled_message_post":{"rkid":"abc","status":"SUCCESS"}}}`)
	if !cli.handleScheduledMessageMex(events.MexNotificationData{OpName: mexScheduledMessagePost}, post) {
		t.Fatal("post not handled")
	}
	rk := base64.RawURLEncoding.EncodeToString(testRevealKey)
	reveal := []byte(`{"data":{"xwa2_notify_scheduled_message_reveal":{"chat_jid":"5491100000000@s.whatsapp.net","rk":"` +
		rk + `","rkid":"abc","st":"1792494000"}}}`)
	if !cli.handleScheduledMessageMex(events.MexNotificationData{OpName: mexScheduledMessageReveal}, reveal) {
		t.Fatal("reveal not handled")
	}
	if cli.handleScheduledMessageMex(events.MexNotificationData{OpName: "NotificationNewsletterJoin"}, post) {
		t.Fatal("other operations must fall through")
	}

	if len(got) != 2 {
		t.Fatalf("got %d events", len(got))
	}
	if p, ok := got[0].(*events.ScheduledMessagePost); !ok || p.RevealKeyID != "abc" || p.Status != "SUCCESS" {
		t.Fatalf("unexpected post event %#v", got[0])
	}
	r, ok := got[1].(*events.ScheduledMessageReveal)
	if !ok || !bytes.Equal(r.RevealKey, testRevealKey) || r.Chat.User != "5491100000000" ||
		!r.ScheduledTimestamp.Equal(time.Unix(1792494000, 0)) {
		t.Fatalf("unexpected reveal event %#v", got[1])
	}
}
