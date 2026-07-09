package costguard

import (
	"errors"
	"fmt"
	"sync"

	"keydeck.local/feasibilitylab/internal/protocol"
)

var ErrInvalidConfig = errors.New("invalid burn-guard configuration")

type Config struct {
	LargeWriteMin    int64
	LowReadMax       int64
	ConsecutiveLimit int
	ExtremeWriteMin  int64
}

type Decision struct {
	Triggered bool
	Reason    string
	Count     int
}

type Guard struct {
	mu          sync.Mutex
	cfg         Config
	consecutive int
	blocked     bool
	reason      string
}

func New(cfg Config) (*Guard, error) {
	if cfg.LargeWriteMin <= 0 || cfg.LowReadMax < 0 || cfg.ConsecutiveLimit <= 0 || cfg.ExtremeWriteMin <= 0 {
		return nil, ErrInvalidConfig
	}
	return &Guard{cfg: cfg}, nil
}

func (g *Guard) Observe(u protocol.Usage) Decision {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked {
		return Decision{Triggered: true, Reason: g.reason, Count: g.consecutive}
	}

	if u.CacheCreationInputTokens >= g.cfg.ExtremeWriteMin && u.CacheReadInputTokens <= g.cfg.LowReadMax {
		g.blocked = true
		g.reason = fmt.Sprintf("extreme cache write: write=%d read=%d", u.CacheCreationInputTokens, u.CacheReadInputTokens)
		return Decision{Triggered: true, Reason: g.reason, Count: g.consecutive}
	}

	largeMiss := u.CacheCreationInputTokens >= g.cfg.LargeWriteMin && u.CacheReadInputTokens <= g.cfg.LowReadMax
	if largeMiss {
		g.consecutive++
	} else {
		g.consecutive = 0
	}
	if g.consecutive >= g.cfg.ConsecutiveLimit {
		g.blocked = true
		g.reason = fmt.Sprintf("consecutive cache misses: count=%d last_write=%d last_read=%d", g.consecutive, u.CacheCreationInputTokens, u.CacheReadInputTokens)
		return Decision{Triggered: true, Reason: g.reason, Count: g.consecutive}
	}
	return Decision{Count: g.consecutive}
}

func (g *Guard) Blocked() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blocked, g.reason
}
