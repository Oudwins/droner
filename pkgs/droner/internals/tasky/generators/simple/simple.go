package simple

import (
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"time"
)

type Generator[T ~string] struct {
	counter uint64
}

func New[T ~string]() *Generator[T] {
	return &Generator[T]{}
}

func (g *Generator[T]) Next(jobID T) string {
	ts := time.Now().UTC().UnixNano()
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		counter := atomic.AddUint64(&g.counter, 1)
		return fmt.Sprintf("%d-%d", ts, counter)
	}
	counter := atomic.AddUint64(&g.counter, 1)
	return fmt.Sprintf("%d-%x-%d", ts, buf[:], counter)
}
