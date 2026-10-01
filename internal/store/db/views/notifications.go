package views

import (
	"encoding/json"
	"fmt"
)

// NotifiedThread is one thread's unread post notifications that a viewer
// may be offered: the thread, its forum, the log length its projection
// reflects, and the posts (TKT-01M29CRJ2T). MeCounts reports them for /me,
// and the notification list builds them from its page, so both hand
// threads.Service.VisibleNotifications the same shape.
type NotifiedThread struct {
	Thread    int64   `json:"thread"`
	Forum     int64   `json:"forum"`
	Projected int64   `json:"projected"`
	Ordinals  []int64 `json:"ordinals"`
}

// ParseNotified reads MeCounts's notified_posts column. An empty column is
// none.
func ParseNotified(s string) ([]NotifiedThread, error) {
	if s == "" {
		return nil, nil
	}
	var out []NotifiedThread
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("views: reading notified_posts: %w", err)
	}
	return out, nil
}
