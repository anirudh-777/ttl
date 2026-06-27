package ws

import (
	"encoding/json"

	"github.com/anirudhprakash/ttl/internal/events"
)

func jsonMarshal(e events.Event) ([]byte, error) {
	return json.Marshal(e)
}
