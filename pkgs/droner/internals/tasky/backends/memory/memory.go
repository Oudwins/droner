package memory

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

var ErrRetriesExceeded = errors.New("retries exceeded")

type Config struct {
	BatchMaxSize int
	BatchMaxWait time.Duration
	RetryDelay   func(attempts int) time.Duration
	RetryMax     int
}

type Backend[T tasky.JobID] struct {
	mu         sync.Mutex
	pending    priorityQueue[T]
	inFlight   map[tasky.TaskID]*queueItem[T]
	batch      []queueItem[T]
	batchTimer *time.Timer
	signal     chan struct{}
	seq        uint64
	cfg        Config
}

func New[T tasky.JobID](cfg Config) *Backend[T] {
	backend := &Backend[T]{
		pending:  priorityQueue[T]{},
		inFlight: make(map[tasky.TaskID]*queueItem[T]),
		signal:   make(chan struct{}, 1),
		cfg:      cfg,
	}
	heap.Init(&backend.pending)
	return backend
}

func (b *Backend[T]) Enqueue(ctx context.Context, task tasky.Task[T], job tasky.Job[T]) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	item := queueItem[T]{
		jobID:    task.JobID,
		taskID:   task.TaskID,
		payload:  task.Payload,
		priority: job.Priority,
		seq:      b.nextSeq(),
	}

	if b.batchEnabledLocked() {
		b.batch = append(b.batch, item)
		if b.cfg.BatchMaxSize > 0 && len(b.batch) >= b.cfg.BatchMaxSize {
			b.flushBatchLocked()
			return nil
		}
		if b.cfg.BatchMaxWait > 0 && b.batchTimer == nil {
			b.batchTimer = time.AfterFunc(b.cfg.BatchMaxWait, b.flushBatchFromTimer)
		}
		return nil
	}

	heap.Push(&b.pending, &item)
	b.signalLocked()
	return nil
}

func (b *Backend[T]) Dequeue(ctx context.Context) (T, tasky.TaskID, []byte, error) {
	for {
		if ctx.Err() != nil {
			var zero T
			return zero, nil, nil, ctx.Err()
		}

		b.mu.Lock()
		if b.pending.Len() > 0 {
			item := heap.Pop(&b.pending).(*queueItem[T])
			b.inFlight[item.taskID] = item
			if b.pending.Len() > 0 {
				b.signalLocked()
			}
			b.mu.Unlock()
			return item.jobID, item.taskID, item.payload, nil
		}
		b.mu.Unlock()

		select {
		case <-ctx.Done():
			var zero T
			return zero, nil, nil, ctx.Err()
		case <-b.signal:
		}
	}
}

func (b *Backend[T]) Ack(ctx context.Context, taskID tasky.TaskID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.inFlight[taskID]; !ok {
		return fmt.Errorf("unknown task id: %v", taskID)
	}
	delete(b.inFlight, taskID)
	return nil
}

func (b *Backend[T]) Nack(ctx context.Context, taskID tasky.TaskID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	b.mu.Lock()
	item, ok := b.inFlight[taskID]
	if !ok {
		b.mu.Unlock()
		return fmt.Errorf("unknown task id: %v", taskID)
	}
	delete(b.inFlight, taskID)
	item.attempts++
	if b.cfg.RetryMax >= 0 && item.attempts > b.cfg.RetryMax {
		b.mu.Unlock()
		return ErrRetriesExceeded
	}
	item.seq = b.nextSeq()
	if b.cfg.RetryDelay != nil {
		delay := b.cfg.RetryDelay(item.attempts)
		if delay <= 0 {
			heap.Push(&b.pending, item)
			b.signalLocked()
			b.mu.Unlock()
			return nil
		}
		itemCopy := *item
		b.mu.Unlock()
		time.AfterFunc(delay, func() {
			b.enqueueItem(itemCopy)
		})
		return nil
	}

	heap.Push(&b.pending, item)
	b.signalLocked()
	b.mu.Unlock()
	return nil
}

func (b *Backend[T]) ForceFlush(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.mu.Lock()
	b.flushBatchLocked()
	b.mu.Unlock()
	return nil
}

func (b *Backend[T]) enqueueItem(item queueItem[T]) {
	b.mu.Lock()
	itemCopy := item
	heap.Push(&b.pending, &itemCopy)
	b.signalLocked()
	b.mu.Unlock()
}

func (b *Backend[T]) flushBatchFromTimer() {
	b.mu.Lock()
	b.flushBatchLocked()
	b.mu.Unlock()
}

func (b *Backend[T]) flushBatchLocked() {
	if len(b.batch) == 0 {
		if b.batchTimer != nil {
			b.batchTimer.Stop()
			b.batchTimer = nil
		}
		return
	}

	for i := range b.batch {
		item := b.batch[i]
		heap.Push(&b.pending, &item)
	}
	b.batch = b.batch[:0]

	if b.batchTimer != nil {
		b.batchTimer.Stop()
		b.batchTimer = nil
	}

	b.signalLocked()
}

func (b *Backend[T]) batchEnabledLocked() bool {
	return b.cfg.BatchMaxSize > 0 || b.cfg.BatchMaxWait > 0
}

func (b *Backend[T]) signalLocked() {
	select {
	case b.signal <- struct{}{}:
	default:
	}
}

func (b *Backend[T]) nextSeq() uint64 {
	b.seq++
	return b.seq
}

type queueItem[T tasky.JobID] struct {
	jobID    T
	taskID   tasky.TaskID
	payload  []byte
	priority int
	seq      uint64
	attempts int
}

type priorityQueue[T tasky.JobID] []*queueItem[T]

func (q priorityQueue[T]) Len() int { return len(q) }

func (q priorityQueue[T]) Less(i, j int) bool {
	if q[i].priority == q[j].priority {
		return q[i].seq < q[j].seq
	}
	return q[i].priority > q[j].priority
}

func (q priorityQueue[T]) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
}

func (q *priorityQueue[T]) Push(x any) {
	*q = append(*q, x.(*queueItem[T]))
}

func (q *priorityQueue[T]) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	*q = old[:n-1]
	return item
}
