package group

import "sync"

// Book is a sharded counter used by the grouping benchmark and by callers that
// only need occurrence counts. Incident state lives in the store, which uses
// the same shard count and Fingerprint function.
const ShardCount = 32

// Book counts occurrences of a fingerprint with one mutex per shard.
type Book struct {
	shards [ShardCount]shard
}

type shard struct {
	mu sync.Mutex
	n  map[uint64]int
}

// Add records one observation and returns the new count and whether it was the
// first time this fingerprint was seen.
func (b *Book) Add(fp uint64) (count int, created bool) {
	s := &b.shards[fp%ShardCount]
	s.mu.Lock()
	if s.n == nil {
		s.n = make(map[uint64]int)
	}
	s.n[fp]++
	count = s.n[fp]
	s.mu.Unlock()
	return count, count == 1
}

// Count returns the current tally.
func (b *Book) Count(fp uint64) int {
	s := &b.shards[fp%ShardCount]
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n == nil {
		return 0
	}
	return s.n[fp]
}
