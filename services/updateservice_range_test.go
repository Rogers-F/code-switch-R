package services

import "testing"

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantStart int64
		wantEnd   int64
		wantTotal int64
		wantOK    bool
	}{
		{name: "valid", value: "bytes 10-99/200", wantStart: 10, wantEnd: 99, wantTotal: 200, wantOK: true},
		{name: "missing total", value: "bytes 10-99/*", wantOK: false},
		{name: "wrong unit", value: "items 10-99/200", wantOK: false},
		{name: "end before start", value: "bytes 99-10/200", wantOK: false},
		{name: "end beyond total", value: "bytes 10-200/200", wantOK: false},
		{name: "malformed", value: "bad", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, total, ok := parseContentRange(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if start != tt.wantStart || end != tt.wantEnd || total != tt.wantTotal {
				t.Fatalf("got %d-%d/%d want %d-%d/%d", start, end, total, tt.wantStart, tt.wantEnd, tt.wantTotal)
			}
		})
	}
}
