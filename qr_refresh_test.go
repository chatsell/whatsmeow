package whatsmeow

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Exercise the notification -> secret rotation -> QR channel path without a
// phone or network. A refreshed QR must carry the same ref and public keys,
// but the current device secret, including when no unused QR refs remain.
func TestCompanionRegistrationRefresh(t *testing.T) {
	for _, tag := range []string{"companion_reg_refresh", "pair-device-rotate-qr"} {
		t.Run(tag, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cli := NewClient(&store.Device{AdvSecretKey: []byte("original-secret")}, waLog.Noop)
			ch, err := cli.GetQRChannel(ctx)
			if err != nil {
				t.Fatal(err)
			}
			initial := "https://wa.me/settings/linked_devices#ref,noise,identity," + base64.StdEncoding.EncodeToString(cli.Store.AdvSecretKey) + ",1"
			cli.dispatchEvent(&events.QR{Codes: []string{initial}})
			receive := func() QRChannelItem {
				t.Helper()
				select {
				case item := <-ch:
					return item
				case <-time.After(time.Second):
					t.Fatal("QR channel did not emit")
					return QRChannelItem{}
				}
			}
			if got := receive(); got.Event != "code" || got.Code != initial {
				t.Fatalf("unexpected initial item: %s", got.Event)
			}
			for i := 0; i < 2; i++ {
				oldSecret := base64.StdEncoding.EncodeToString(cli.Store.AdvSecretKey)
				cli.handleNotification(ctx, &waBinary.Node{Tag: "notification", Attrs: waBinary.Attrs{"from": types.ServerJID, "type": "companion_reg_refresh", "id": "test"}, Content: []waBinary.Node{{Tag: tag}}})
				got := receive()
				parts := strings.Split(got.Code, ",")
				if got.Event != "code" || len(parts) != 5 {
					t.Fatalf("expected refreshed QR, got %s", got.Event)
				}
				if parts[0] != "https://wa.me/settings/linked_devices#ref" || parts[1] != "noise" || parts[2] != "identity" || parts[4] != "1" {
					t.Fatal("refresh changed QR identity or reference")
				}
				if parts[3] == oldSecret || parts[3] != base64.StdEncoding.EncodeToString(cli.Store.AdvSecretKey) {
					t.Fatal("QR does not use the rotated device secret")
				}
				if len(cli.Store.AdvSecretKey) != 32 {
					t.Fatal("incorrect secret size")
				}
			}
			cli.dispatchEvent(&events.PairSuccess{})
			if got := receive(); got.Event != "success" {
				t.Fatalf("pairing did not finish: %s", got.Event)
			}
		})
	}
}
