package cmd

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"cloud-computer-keepalive/internal/chuanyun"
	"cloud-computer-keepalive/internal/spice"
)

type scgCaptureConn struct{ bytes.Buffer }

func (c *scgCaptureConn) Read([]byte) (int, error)         { return 0, nil }
func (c *scgCaptureConn) Close() error                     { return nil }
func (c *scgCaptureConn) LocalAddr() net.Addr              { return nil }
func (c *scgCaptureConn) RemoteAddr() net.Addr             { return nil }
func (c *scgCaptureConn) SetDeadline(time.Time) error      { return nil }
func (c *scgCaptureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scgCaptureConn) SetWriteDeadline(time.Time) error { return nil }

func TestHandleSCGFrameAckWindow(t *testing.T) {
	conn := &scgCaptureConn{}
	states := make(map[uint64]*scgAckState)
	setAck := make([]byte, 14)
	binary.LittleEndian.PutUint16(setAck[0:2], 0x03)
	binary.LittleEndian.PutUint32(setAck[2:6], 8)
	binary.LittleEndian.PutUint32(setAck[6:10], 1)
	binary.LittleEndian.PutUint32(setAck[10:14], 2)
	if err := handleSCGFrame(conn, 9, &chuanyun.Frame{PktType: spice.DataType, Field2: 2, Payload: setAck}, states); err != nil {
		t.Fatal(err)
	}
	conn.Reset()

	message := make([]byte, 6)
	binary.LittleEndian.PutUint16(message[0:2], 0x66)
	for i := 0; i < 2; i++ {
		if err := handleSCGFrame(conn, 9, &chuanyun.Frame{PktType: spice.DataType, Field2: 2, Payload: message}, states); err != nil {
			t.Fatal(err)
		}
	}
	written := conn.Bytes()
	if len(written) < chuanyun.FrameHeadSize+6 {
		t.Fatalf("ACK frame too short: %x", written)
	}
	payload := written[chuanyun.FrameHeadSize:]
	if binary.LittleEndian.Uint16(payload[:2]) != 0x02 {
		t.Fatalf("expected ACK type 0x02, got %x", payload)
	}
}
