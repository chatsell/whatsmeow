package whatsmeow

import (
	"strings"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func TestMentionsScheduling(t *testing.T) {
	cases := []struct {
		name string
		node waBinary.Node
		want bool
	}{
		{"plain message", waBinary.Node{
			Tag:     "message",
			Attrs:   waBinary.Attrs{"id": "1", "type": "text"},
			Content: []waBinary.Node{{Tag: "enc", Attrs: waBinary.Attrs{"type": "skmsg"}}},
		}, false},
		{"reveal child", waBinary.Node{
			Tag:     "message",
			Content: []waBinary.Node{{Tag: "reveal", Attrs: waBinary.Attrs{"key_id": "x"}}},
		}, true},
		{"scheduled attribute deep down", waBinary.Node{
			Tag: "notification",
			Content: []waBinary.Node{{Tag: "a", Content: []waBinary.Node{
				{Tag: "b", Attrs: waBinary.Attrs{"scheduled_time": "1"}},
			}}},
		}, true},
		{"push name containing the word", waBinary.Node{
			Tag:   "message",
			Attrs: waBinary.Attrs{"notify": "Reveal Store", "type": "text"},
		}, false},
		{"scheduled message meta by value", waBinary.Node{
			Tag:     "message",
			Content: []waBinary.Node{{Tag: "meta", Attrs: waBinary.Attrs{"type": "scheduled_message"}}},
		}, true},
		{"beyond depth", waBinary.Node{
			Tag: "notification",
			Content: []waBinary.Node{{Tag: "a", Content: []waBinary.Node{{Tag: "b", Content: []waBinary.Node{
				{Tag: "c", Content: []waBinary.Node{{Tag: "reveal"}}},
			}}}}},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mentionsScheduling(&tc.node, 3); got != tc.want {
				t.Fatalf("mentionsScheduling = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoggableNodeIsBounded(t *testing.T) {
	node := waBinary.Node{Tag: "message", Content: []byte(strings.Repeat("x", 3*maxLoggedNodeLen))}
	if got := loggableNode(&node); len(got) > maxLoggedNodeLen+len("...(truncated)") {
		t.Fatalf("loggableNode returned %d bytes", len(got))
	}
}
