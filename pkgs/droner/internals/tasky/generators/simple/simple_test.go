package simple

import (
	"sync"
	"testing"
	"time"
)

func TestGeneratorIncrements(t *testing.T) {
	gen := New[string]()
	first := gen.Next("job").(uint64)
	second := gen.Next("job").(uint64)
	if second <= first {
		t.Fatalf("expected incrementing values, got %d then %d", first, second)
	}
}

func TestGeneratorConcurrent(t *testing.T) {
	gen := New[string]()
	seen := make(map[uint64]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val := gen.Next("job").(uint64)
			mu.Lock()
			seen[val] = struct{}{}
			mu.Unlock()
		}()
	}
	wt := make(chan struct{})
	go func() {
		wg.Wait()
		close(wt)
	}()
	select {
	case <-wt:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for generator goroutines")
	}

	if len(seen) != 50 {
		t.Fatalf("expected 50 unique values, got %d", len(seen))
	}
}
