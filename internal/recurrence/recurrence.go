package recurrence

import (
	"fmt"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Normalize converts friendly presets to RRULE strings and validates raw rules.
// Empty and "none" clear recurrence.
func Normalize(input string, start time.Time) (string, error) {
	v := strings.TrimSpace(input)
	switch strings.ToLower(v) {
	case "", "none":
		return "", nil
	case "daily":
		v = "FREQ=DAILY"
	case "weekdays":
		v = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
	case "weekly":
		v = "FREQ=WEEKLY"
	case "monthly":
		v = "FREQ=MONTHLY"
	case "yearly":
		v = "FREQ=YEARLY"
	default:
		if strings.HasPrefix(strings.ToLower(v), "rrule:") {
			v = strings.TrimSpace(v[len("rrule:"):])
		}
	}
	if !strings.HasPrefix(strings.ToUpper(v), "RRULE:") {
		v = "RRULE:" + v
	}
	dt := start
	if dt.IsZero() {
		dt = time.Now()
	}
	full := "DTSTART:" + dt.Format("20060102T150405Z") + "\n" + v
	if _, err := rrule.StrToRRule(full); err != nil {
		return "", fmt.Errorf("invalid recurrence %q: %w", input, err)
	}
	return strings.TrimPrefix(v, "RRULE:"), nil
}
