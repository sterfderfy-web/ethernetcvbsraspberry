package mshal

import "testing"

type fakeHID struct { sent int; request []byte; short bool }
func (d *fakeHID) Close() error { return nil }
func (d *fakeHID) SendFeatureReport(b []byte) (int, error) { d.sent++; d.request = append([]byte(nil), b...); return len(b), nil }
func (d *fakeHID) GetFeatureReport(b []byte) (int, error) { copy(b, d.request); if d.short { return 4, nil }; return len(b), nil }

func TestDiagnosticBlocksMutation(t *testing.T) {
    for _, op := range []byte{0xb6, 0xe6, 0xa6, 0xee, 0xef} {
        d := &fakeHID{}
        h := &HAL{dev:d}
        if _, err := h.romExchangeReport([9]byte{0,op}, 0); err == nil || d.sent != 0 { t.Fatalf("opcode %02x was not blocked before I/O",op) }
    }
}
func TestDiagnosticReadsAndRejectsShortReply(t *testing.T) {
    for _, op := range []byte{0xb5, 0xe5, 0xa5} {
        d := &fakeHID{}
        h := &HAL{dev:d}
        if _, err := h.romExchangeReport([9]byte{0,op}, 2); err != nil { t.Fatal(err) }
        d.short = true
        if _, err := h.romExchangeReport([9]byte{0,op}, 2); err == nil { t.Fatal("accepted short reply") }
    }
}
