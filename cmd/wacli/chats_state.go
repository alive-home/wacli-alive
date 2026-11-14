package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/store"
)

func validateBoolFilter(name string, pos, neg bool) error {
	if pos && neg {
		return fmt.Errorf("--%s and --no-%s are mutually exclusive", name, name)
	}
	return nil
}

func boolFilter(pos, neg bool) *bool {
	if pos {
		v := true
		return &v
	}
	if neg {
		v := false
		return &v
	}
	return nil
}

func chatFlagsString(c store.Chat) string {
	var flags []string
	if c.Pinned {
		flags = append(flags, "pinned")
	}
	if c.Archived {
		flags = append(flags, "archived")
	}
	if c.Muted() {
		flags = append(flags, "muted")
	}
	if c.Unread {
		if c.UnreadCount > 0 {
			flags = append(flags, fmt.Sprintf("unread:%d", c.UnreadCount))
		} else {
			flags = append(flags, "unread")
		}
	}
	return strings.Join(flags, ",")
}

func formatMutedUntil(until int64) string {
	switch {
	case until == -1:
		return "forever"
	case until > 0:
		return time.Unix(until, 0).Local().Format(time.RFC3339)
	default:
		return ""
	}
}
