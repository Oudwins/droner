package simple

import (
	"sync"
	"testing"
	"time"
)

func TestGeneratorIncrements(t *testing.T) {
	gen := New[string]()
	first := gen.Next("job")
	second := gen.Next("job")
	if first == second {
		t.Fatal("expected unique values")
	}
}

func TestGeneratorConcurrent(t *testing.T) {
	gen := New[string]()
	seen := make(map[string]struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value := gen.Next("job")
			mu.Lock()
			seen[value] = struct{}{}
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
