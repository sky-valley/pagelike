package store

import "testing"

func TestParseEventID(t *testing.T) {
	for _, c := range []struct {
		id      string
		ms, seq int64
		ok      bool
	}{
		{"1790000000123-0", 1790000000123, 0, true},
		{"1000-17", 1000, 17, true},
		{"not-an-event-id", 0, 0, false},
		{"12", 0, 0, false},
		{"-1-2", 0, 0, false},
		{"1-+2", 0, 0, false},
		{"", 0, 0, false},
	} {
		ms, seq, ok := ParseEventID(c.id)
		if ok != c.ok || ms != c.ms || seq != c.seq {
			t.Errorf("ParseEventID(%q) = %d %d %v", c.id, ms, seq, ok)
		}
	}
	if got := (Event{TimeMS: 5, Seq: 9}).ID(); got != "5-9" {
		t.Errorf("ID %q", got)
	}
}
