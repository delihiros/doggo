package vm

import (
	"testing"
	"time"
)

func TestParseTimestampValueRollsLeapSecondForward(t *testing.T) {
	parsed, offset, err := parseTimestampValue("2015-06-30T23:59:60Z")
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 {
		t.Fatalf("offset = %d", offset)
	}
	if got := parsed.UTC().Format(time.RFC3339Nano); got != "2015-07-01T00:00:00Z" {
		t.Fatalf("leap second = %s", got)
	}
}
