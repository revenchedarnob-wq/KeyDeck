package continuity

import "strings"

type SentenceBuffer struct {
	confirmed strings.Builder
	pending   string
}

func (b *SentenceBuffer) Push(chunk string) string {
	b.pending += chunk
	cut := lastSentenceBoundary(b.pending)
	if cut < 0 {
		return ""
	}
	committed := b.pending[:cut]
	b.confirmed.WriteString(committed)
	b.pending = b.pending[cut:]
	return committed
}

func (b *SentenceBuffer) Confirmed() string { return b.confirmed.String() }
func (b *SentenceBuffer) Pending() string   { return b.pending }

func (b *SentenceBuffer) Flush() string {
	remaining := b.pending
	b.confirmed.WriteString(remaining)
	b.pending = ""
	return remaining
}

func lastSentenceBoundary(s string) int {
	last := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.', '!', '?':
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\n' || s[j] == '\t') {
				j++
			}
			last = j
		}
	}
	return last
}
