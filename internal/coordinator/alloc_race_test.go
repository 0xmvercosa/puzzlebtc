package coordinator

import (
	"context"
	"sync"
	"testing"
)

// Allocation must be safe under the concurrency it will actually see. An earlier
// version kept a *math/rand.Rand on the Coordinator and drew from it without a
// lock, which go test -race flags as soon as a second participant connects. A
// corrupted rand source can repeat indices, and repeating an index is exactly
// the guarantee this project sells.
func TestConcurrentLeasesAreRaceFree(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	const workers, each = 8, 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[uint64]string)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := string(rune('a' + w))
			for i := 0; i < each; i++ {
				lease, err := co.LeaseBlock(ctx, id)
				if err != nil {
					t.Errorf("worker %s: %v", id, err)
					return
				}
				mu.Lock()
				if prev, dup := seen[leaseIndex(lease)]; dup {
					t.Errorf("block %d leased to both %s and %s", leaseIndex(lease), prev, id)
				}
				seen[leaseIndex(lease)] = id
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if len(seen) != workers*each {
		t.Errorf("got %d distinct blocks, want %d", len(seen), workers*each)
	}
}
