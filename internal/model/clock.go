package model

import "time"

// Clock is the only source of "now" (D12).
type Clock interface {
	Now() time.Time
}
