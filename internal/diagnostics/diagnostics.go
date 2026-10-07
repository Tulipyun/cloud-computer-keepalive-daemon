package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	maxJournalBytes = 64 << 20
	journalCopies   = 5
	ringCapacity    = 512
	incidentDataMax = 8192
)

// BuildLabel identifies the diagnostic build in session.json. Release builds
// override it with -ldflags so the recorded label matches the published asset
// name instead of a hard-coded historical version.
var BuildLabel = "v0.2.0-longtest-1"

type Session struct {
	Dir        string
	RuntimeLog string
}

type PacketRecord struct {
	Sequence  uint64 `json:"sequence"`
	At        string `json:"at"`
	Direction string `json:"direction"`
	Layer     string `json:"layer"`
	Channel   string `json:"channel,omitempty"`
	Type      string `json:"type,omitempty"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	HeadHex   string `json:"headHex,omitempty"`
	TailHex   string `json:"tailHex,omitempty"`
	DataHex   string `json:"dataHex,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type recorder struct {
	mu      sync.Mutex
	dir     string
	events  *rotatingJSONL
	packets *rotatingJSONL
	ring    []PacketRecord
	ringPos int
	seq     uint64
}

type rotatingJSONL struct {
	path   string
	file   *os.File
	size   int64
	max    int64
	copies int
}

var active *recorder
var activeMu sync.RWMutex

func Start(baseDir string) (*Session, error) {
	if baseDir == "" {
		baseDir = "logs"
	}
	stamp := time.Now().Format("20060102-150405.000")
	dir := filepath.Join(baseDir, "session-"+stamp)
	if err := os.MkdirAll(filepath.Join(dir, "incidents"), 0700); err != nil {
		return nil, err
	}
	events, err := openRotatingJSONL(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	packets, err := openRotatingJSONL(filepath.Join(dir, "packets.jsonl"))
	if err != nil {
		_ = events.close()
		return nil, err
	}
	r := &recorder{dir: dir, events: events, packets: packets, ring: make([]PacketRecord, 0, ringCapacity)}
	activeMu.Lock()
	active = r
	activeMu.Unlock()
	metadata := map[string]any{
		"startedAt":  time.Now().Format(time.RFC3339Nano),
		"buildLabel": BuildLabel,
		"schema":     1,
		"pid":        os.Getpid(),
		"goVersion":  runtime.Version(),
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"args":       os.Args,
	}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0600); err != nil {
		Close()
		return nil, err
	}
	Event("diagnostic_session_started", map[string]any{"directory": dir})
	return &Session{Dir: dir, RuntimeLog: filepath.Join(dir, "runtime.log")}, nil
}

func Close() {
	activeMu.Lock()
	r := active
	active = nil
	activeMu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.events.close()
	_ = r.packets.close()
}

func Event(name string, fields map[string]any) {
	r := current()
	if r == nil {
		return
	}
	record := map[string]any{"at": time.Now().Format(time.RFC3339Nano), "event": name}
	for key, value := range fields {
		record[key] = value
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.events.write(record)
}

func Packet(direction, layer, channel string, msgType uint64, data []byte) {
	r := current()
	if r == nil {
		return
	}
	hash := sha256.Sum256(data)
	record := PacketRecord{
		At:        time.Now().Format(time.RFC3339Nano),
		Direction: direction,
		Layer:     layer,
		Channel:   channel,
		Type:      fmt.Sprintf("0x%x", msgType),
		Size:      len(data),
		SHA256:    hex.EncodeToString(hash[:]),
		HeadHex:   hex.EncodeToString(prefix(data, 256)),
		TailHex:   hex.EncodeToString(suffix(data, 64)),
	}
	incidentData := data
	if len(incidentData) > incidentDataMax {
		incidentData = append(append([]byte(nil), prefix(data, incidentDataMax/2)...), suffix(data, incidentDataMax/2)...)
		record.Truncated = true
	}
	record.DataHex = hex.EncodeToString(incidentData)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	record.Sequence = r.seq
	summary := record
	summary.DataHex = ""
	_ = r.packets.write(summary)
	if len(r.ring) < ringCapacity {
		r.ring = append(r.ring, record)
	} else {
		r.ring[r.ringPos] = record
		r.ringPos = (r.ringPos + 1) % ringCapacity
	}
}

func Incident(reason error, fields map[string]any) string {
	r := current()
	if r == nil {
		return ""
	}
	stack := make([]byte, 1<<20)
	n := runtime.Stack(stack, true)
	r.mu.Lock()
	packets := r.orderedRingLocked()
	stamp := time.Now().Format("20060102-150405.000000")
	path := filepath.Join(r.dir, "incidents", "incident-"+stamp+".json")
	record := map[string]any{
		"at":         time.Now().Format(time.RFC3339Nano),
		"error":      fmt.Sprint(reason),
		"fields":     fields,
		"packets":    packets,
		"goroutines": string(stack[:n]),
	}
	data, _ := json.MarshalIndent(record, "", "  ")
	_ = os.WriteFile(path, data, 0600)
	_ = r.events.write(map[string]any{"at": time.Now().Format(time.RFC3339Nano), "event": "incident", "path": path, "error": fmt.Sprint(reason)})
	r.mu.Unlock()
	return path
}

func SessionDir() string {
	r := current()
	if r == nil {
		return ""
	}
	return r.dir
}

func RuntimeSnapshot() map[string]any {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return map[string]any{
		"goroutines":       runtime.NumGoroutine(),
		"heapAllocBytes":   memory.HeapAlloc,
		"heapObjects":      memory.HeapObjects,
		"systemBytes":      memory.Sys,
		"gcCycles":         memory.NumGC,
		"lastGCPauseNanos": memory.PauseNs[(memory.NumGC+255)%256],
	}
}

func current() *recorder {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return active
}

func (r *recorder) orderedRingLocked() []PacketRecord {
	if len(r.ring) < ringCapacity || r.ringPos == 0 {
		return append([]PacketRecord(nil), r.ring...)
	}
	out := make([]PacketRecord, 0, len(r.ring))
	out = append(out, r.ring[r.ringPos:]...)
	out = append(out, r.ring[:r.ringPos]...)
	return out
}

func openRotatingJSONL(path string) (*rotatingJSONL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return &rotatingJSONL{path: path, file: f, size: size, max: maxJournalBytes, copies: journalCopies}, nil
}

func (w *rotatingJSONL) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if w.size+int64(len(data)) > w.max {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	return err
}

func (w *rotatingJSONL) rotate() error {
	_ = w.file.Sync()
	_ = w.file.Close()
	for i := w.copies - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1))
	}
	_ = os.Rename(w.path, w.path+".1")
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

func (w *rotatingJSONL) close() error {
	if w == nil || w.file == nil {
		return nil
	}
	_ = w.file.Sync()
	return w.file.Close()
}

func prefix(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[:n]
}

func suffix(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[len(data)-n:]
}
