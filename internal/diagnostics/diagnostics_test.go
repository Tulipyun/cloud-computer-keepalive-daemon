package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIncidentContainsPacketWindow(t *testing.T) {
	session, err := Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	Packet("rx", "test", "display", 0x66, []byte{1, 2, 3})
	path := Incident(errors.New("test failure"), map[string]any{"kind": "protocol"})
	if filepath.Dir(filepath.Dir(path)) != session.Dir {
		t.Fatalf("incident path %q not under session %q", path, session.Dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Error   string         `json:"error"`
		Packets []PacketRecord `json:"packets"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Error != "test failure" || len(record.Packets) != 1 {
		t.Fatalf("unexpected incident: %+v", record)
	}
	if record.Packets[0].DataHex != "010203" {
		t.Fatalf("unexpected packet data: %+v", record.Packets[0])
	}
}
