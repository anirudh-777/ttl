package ws

import (
	"encoding/json"

	"github.com/anirudh-777/ttl/internal/events"
)

func jsonMarshal(e events.Event) ([]byte, error) {
	return json.Marshal(e)
}
