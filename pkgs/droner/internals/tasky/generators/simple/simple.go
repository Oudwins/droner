package simple

import (
	"sync/atomic"
)

type Generator[T ~string] struct {
	counter uint64
}

func New[T ~string]() *Generator[T] {
	return &Generator[T]{}
}

func (g *Generator[T]) Next(jobID T) any {
	return atomic.AddUint64(&g.counter, 1)
}
