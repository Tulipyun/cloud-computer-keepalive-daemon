package cmd

import (
	"errors"
	"testing"
	"time"

	"cloud-computer-keepalive/internal/spice"
)

func TestZTESessionHealthReady(t *testing.T) {
	health := newZTESessionHealth(map[byte]bool{5: true, 7: true})
	health.markDisplayInit(5)
	health.observe(5, &spice.RawState{Messages: 3, MarkReceived: true})
	ready, evidence := health.ready()
	if !ready || evidence == "" {
		t.Fatalf("expected display readiness, ready=%v evidence=%q", ready, evidence)
	}
}

func TestZTESessionHealthAllDisplaysClosed(t *testing.T) {
	health := newZTESessionHealth(map[byte]bool{5: true, 7: true})
	health.linkClosed(5, errors.New("first closed"))
	select {
	case err := <-health.errCh:
		t.Fatalf("reported failure while one display remained: %v", err)
	default:
	}
	health.linkClosed(7, errors.New("second closed"))
	select {
	case err := <-health.errCh:
		if err == nil {
			t.Fatal("expected display close error")
		}
	case <-time.After(time.Second):
		t.Fatal("display close error was not propagated")
	}
}
