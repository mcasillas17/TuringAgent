package auth

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	authFailureQueueCapacity = 64
	authFailureMinInterval   = time.Second
)

var errFailureRecorderClosed = errors.New("authentication failure recorder is closed")

type AsyncFailureRecorder struct {
	recorder FailureRecorder
	queue    chan Failure
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}

	mu      sync.RWMutex
	closed  bool
	dropped atomic.Uint64
}

func NewAsyncFailureRecorder(recorder FailureRecorder) *AsyncFailureRecorder {
	ctx, cancel := context.WithCancel(context.Background())
	async := &AsyncFailureRecorder{
		recorder: recorder,
		queue:    make(chan Failure, authFailureQueueCapacity),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go async.run()
	return async
}

func (r *AsyncFailureRecorder) Record(_ context.Context, failure Failure) error {
	if r == nil || r.recorder == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errFailureRecorderClosed
	}
	select {
	case r.queue <- failure:
	default:
		r.dropped.Add(1)
	}
	return nil
}

func (r *AsyncFailureRecorder) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.cancel()
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *AsyncFailureRecorder) run() {
	defer close(r.done)
	nextRecord := time.Time{}
	for {
		select {
		case <-r.ctx.Done():
			r.logDropped()
			return
		case failure := <-r.queue:
			if wait := time.Until(nextRecord); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-r.ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					r.logDropped()
					return
				case <-timer.C:
				}
			}
			auditCtx, cancel := context.WithTimeout(r.ctx, authAuditTimeout)
			err := r.recorder(auditCtx, failure)
			cancel()
			if err != nil && r.ctx.Err() == nil {
				log.Printf("record authentication failure for %s: %v", failure.Method, err)
			}
			nextRecord = time.Now().Add(authFailureMinInterval)
			r.logDropped()
		}
	}
}

func (r *AsyncFailureRecorder) logDropped() {
	if dropped := r.dropped.Swap(0); dropped > 0 {
		log.Printf("dropped %d authentication failure audit records due to rate limiting", dropped)
	}
}
