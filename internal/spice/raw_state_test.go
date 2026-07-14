package spice

import (
	"bytes"
	"net"
	"testing"
	"time"
)

type captureConn struct {
	bytes.Buffer
	err error
}

func (c *captureConn) Read([]byte) (int, error)         { return 0, c.err }
func (c *captureConn) Close() error                     { return nil }
func (c *captureConn) LocalAddr() net.Addr              { return nil }
func (c *captureConn) RemoteAddr() net.Addr             { return nil }
func (c *captureConn) SetDeadline(time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(time.Time) error { return nil }

func TestRawStateAckWindow(t *testing.T) {
	conn := &captureConn{}
	state := &RawState{LastSerial: 7}
	setAck := []byte{1, 0, 0, 0, 2, 0, 0, 0}

	if replied, err := state.HandleMessage(conn, 0x03, setAck); err != nil || !replied {
		t.Fatalf("SET_ACK reply = %v, %v", replied, err)
	}
	if !state.SetAckReceived || !state.AckSyncSent || state.AckWindow != 2 {
		t.Fatalf("unexpected ACK state: %+v", state)
	}
	conn.Reset()

	if replied, err := state.HandleMessage(conn, 0x66, nil); err != nil || replied {
		t.Fatalf("first window message reply = %v, %v", replied, err)
	}
	if replied, err := state.HandleMessage(conn, 0x130, nil); err != nil || !replied {
		t.Fatalf("second window message reply = %v, %v", replied, err)
	}
	written := conn.Bytes()
	if len(written) < 14 || written[8] != 0x02 || written[9] != 0x00 {
		t.Fatalf("expected ACK message, got %x", written)
	}
	if state.AckSent != 1 || state.AckPending != 0 {
		t.Fatalf("unexpected ACK counters: %+v", state)
	}
}

func TestRawStateDisplayProgress(t *testing.T) {
	state := &RawState{}
	conn := &captureConn{}
	_, _ = state.HandleMessage(conn, 0x13a, nil)
	_, _ = state.HandleMessage(conn, 0x66, nil)
	if !state.DisplayReady() || !state.SurfaceCreated || !state.MarkReceived {
		t.Fatalf("display progress not recorded: %+v", state)
	}
}
