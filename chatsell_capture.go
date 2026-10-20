// Chatsell patch, not upstream.
//
// whatsmeow drops several kinds of stanza with only a Debug line, or with no
// line at all, so a new WhatsApp feature disappears without a trace at the
// default log level. WhatsApp scheduled messages are the case at hand: the
// encrypted body arrives when the message is scheduled, and the way its reveal
// key arrives at the scheduled time is unknown. These helpers make those
// stanzas visible so it can be captured. Drop them once the reveal is
// implemented.

package whatsmeow

import (
	"fmt"
	"regexp"
	"strings"

	waBinary "go.mau.fi/whatsmeow/binary"
)

const maxLoggedNodeLen = 8192

// loggableNode renders a node as XML without reveal keys, truncated to keep a
// single log line bounded.
func loggableNode(node *waBinary.Node) string {
	redacted := redactRevealKeys(*node)
	s := redacted.String()
	if len(s) > maxLoggedNodeLen {
		s = s[:maxLoggedNodeLen] + "...(truncated)"
	}
	return s
}

// revealKeyJSON matches the reveal key of a scheduled message mex payload.
var revealKeyJSON = regexp.MustCompile(`"rk"\s*:\s*"[^"]*"`)

// redactRevealKeys returns a copy of node without scheduled message reveal
// keys: they decrypt the message body, so they never go to the logs.
func redactRevealKeys(node waBinary.Node) waBinary.Node {
	switch content := node.Content.(type) {
	case []waBinary.Node:
		children := make([]waBinary.Node, len(content))
		for i, child := range content {
			children[i] = redactRevealKeys(child)
		}
		node.Content = children
	case []byte:
		if node.Tag == "key" {
			node.Content = []byte(fmt.Sprintf("[redacted %d bytes]", len(content)))
		} else if revealKeyJSON.Match(content) {
			node.Content = revealKeyJSON.ReplaceAll(content, []byte(`"rk":"[redacted]"`))
		}
	}
	return node
}

// mentionsScheduling reports whether a tag or attribute name in the node tree,
// up to depth levels below it, refers to scheduling or revealing a message.
func mentionsScheduling(node *waBinary.Node, depth int) bool {
	if hasSchedulingWord(node.Tag) {
		return true
	}
	for key, value := range node.Attrs {
		if hasSchedulingWord(key) {
			return true
		}
		// The scheduled message markers are values: type="scheduled_message"
		// and op_name="NotificationScheduledMessage…". Only those two
		// attributes are checked, so push names or group subjects that happen
		// to contain the words don't log every message of that chat.
		if key == "type" || key == "op_name" {
			if text, ok := value.(string); ok && (strings.Contains(text, "chedul") || strings.Contains(text, "eveal")) {
				return true
			}
		}
	}
	if depth == 0 {
		return false
	}
	for _, child := range node.GetChildren() {
		if mentionsScheduling(&child, depth-1) {
			return true
		}
	}
	return false
}

// hasSchedulingWord checks a tag or attribute name. Protocol names are
// lowercase, so there is no need to allocate a lowercased copy per frame.
func hasSchedulingWord(s string) bool {
	return strings.Contains(s, "reveal") || strings.Contains(s, "schedul")
}
