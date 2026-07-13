package recurrence

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizePresetsAndRaw(t *testing.T) {
	start := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	cases := map[string]string{"daily": "FREQ=DAILY", "weekdays": "BYDAY=MO,TU,WE,TH,FR", "rrule:FREQ=WEEKLY;BYDAY=MO": "FREQ=WEEKLY"}
	for input, want := range cases {
		got, err := Normalize(input, start)
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("Normalize(%q)=%q err=%v", input, got, err)
		}
	}
	if _, err := Normalize("not-a-rule", start); err == nil {
		t.Fatal("expected invalid rule")
	}
}
