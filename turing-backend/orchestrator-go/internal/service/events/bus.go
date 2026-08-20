package events

import "sync"

const maxTerminatedSessionFences = 1024

type Event struct {
	EventID     string
	SessionID   string
	RunID       string
	TraceID     string
	Sequence    int64
	Type        string
	CreatedAt   string
	PayloadJSON string
}

type Bus struct {
	mu                 sync.Mutex
	bufferSize         int
	nextID             int64
	subs               map[int64]subscription
	terminatedSessions map[string]struct{}
	terminatedOrder    []string
}

type subscription struct {
	sessionID string
	stream    *sessionEventSubscription
	updates   *sessionUpdateSubscription
}

func NewBus(bufferSize int) *Bus {
	return &Bus{
		bufferSize:         bufferSize,
		subs:               map[int64]subscription{},
		terminatedSessions: map[string]struct{}{},
	}
}

func (b *Bus) Subscribe(sessionID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	stream := newSessionEventSubscription(b.bufferSize)
	b.subs[id] = subscription{sessionID: sessionID, stream: stream}
	return stream.out, func() {
		b.mu.Lock()
		sub, ok := b.subs[id]
		if !ok {
			b.mu.Unlock()
			return
		}
		delete(b.subs, id)
		b.mu.Unlock()
		sub.stream.stop()
	}
}

// SessionSubscriberCount is a content-free liveness observation for integration
// tests and orchestration diagnostics. It does not expose subscriber identity
// or event data.
func (b *Bus) SessionSubscriberCount(sessionID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for _, sub := range b.subs {
		if sub.stream != nil && sub.sessionID == sessionID {
			count++
		}
	}
	return count
}

func (b *Bus) SubscribeSessionUpdates() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	updates := newSessionUpdateSubscription()
	b.subs[id] = subscription{updates: updates}
	return updates.out, func() {
		b.mu.Lock()
		sub, ok := b.subs[id]
		if ok {
			delete(b.subs, id)
		}
		b.mu.Unlock()
		if ok {
			sub.updates.stop()
		}
	}
}

func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, terminated := b.terminatedSessions[event.SessionID]; terminated {
		return
	}
	for _, sub := range b.subs {
		if sub.updates != nil {
			sub.updates.publish(event)
			continue
		}
		if sub.sessionID != event.SessionID {
			continue
		}
		sub.stream.publish(event)
	}
}

// TerminateSession delivers one terminal event through a dedicated one-shot
// path. The ordinary event buffer may overflow, but it cannot evict this event.
func (b *Bus) TerminateSession(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, terminated := b.terminatedSessions[event.SessionID]; terminated {
		return
	}
	b.terminatedSessions[event.SessionID] = struct{}{}
	b.terminatedOrder = append(b.terminatedOrder, event.SessionID)
	if len(b.terminatedOrder) > maxTerminatedSessionFences {
		expired := b.terminatedOrder[0]
		delete(b.terminatedSessions, expired)
		b.terminatedOrder = b.terminatedOrder[1:]
	}
	for _, sub := range b.subs {
		if sub.updates != nil {
			sub.updates.publish(event)
			continue
		}
		if sub.stream != nil && sub.sessionID == event.SessionID {
			sub.stream.terminate(event)
		}
	}
}

type sessionEventSubscription struct {
	ordinary     chan Event
	terminal     chan Event
	out          chan Event
	done         chan struct{}
	stopOnce     sync.Once
	terminalOnce sync.Once
}

func newSessionEventSubscription(bufferSize int) *sessionEventSubscription {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	sub := &sessionEventSubscription{
		ordinary: make(chan Event, bufferSize),
		terminal: make(chan Event, 1),
		out:      make(chan Event, bufferSize),
		done:     make(chan struct{}),
	}
	go sub.run()
	return sub
}

func (s *sessionEventSubscription) publish(event Event) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.ordinary <- event:
	default:
		latest := event
		draining := true
		for draining {
			select {
			case queued := <-s.ordinary:
				if queued.Sequence >= latest.Sequence {
					latest = queued
				}
			default:
				draining = false
			}
		}
		select {
		case s.ordinary <- latest:
		default:
		}
	}
}

func (s *sessionEventSubscription) terminate(event Event) {
	s.terminalOnce.Do(func() {
		s.terminal <- event
	})
}

func (s *sessionEventSubscription) run() {
	defer close(s.out)
	for {
		select {
		case <-s.done:
			return
		case terminal := <-s.terminal:
			s.deliver(terminal)
			return
		default:
		}
		select {
		case <-s.done:
			return
		case terminal := <-s.terminal:
			s.deliver(terminal)
			return
		case event := <-s.ordinary:
			s.deliver(event)
		}
	}
}

func (s *sessionEventSubscription) deliver(event Event) {
	select {
	case <-s.done:
	case s.out <- event:
	}
}

func (s *sessionEventSubscription) stop() {
	s.stopOnce.Do(func() {
		close(s.done)
	})
}

type sessionUpdateSubscription struct {
	mu       sync.Mutex
	pending  map[string]Event
	wake     chan struct{}
	out      chan Event
	done     chan struct{}
	stopOnce sync.Once
}

func newSessionUpdateSubscription() *sessionUpdateSubscription {
	sub := &sessionUpdateSubscription{
		pending: make(map[string]Event),
		wake:    make(chan struct{}, 1),
		out:     make(chan Event),
		done:    make(chan struct{}),
	}
	go sub.run()
	return sub
}

func (s *sessionUpdateSubscription) publish(event Event) {
	if event.Type != "session.updated" && event.Type != "session.deleted" {
		return
	}
	s.mu.Lock()
	previous, found := s.pending[event.SessionID]
	if !found || event.Sequence >= previous.Sequence {
		s.pending[event.SessionID] = event
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *sessionUpdateSubscription) run() {
	defer close(s.out)
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
			for {
				s.mu.Lock()
				var next Event
				for sessionID, event := range s.pending {
					next = event
					delete(s.pending, sessionID)
					break
				}
				s.mu.Unlock()
				if next.SessionID == "" {
					break
				}
				select {
				case <-s.done:
					return
				case s.out <- next:
				}
			}
		}
	}
}

func (s *sessionUpdateSubscription) stop() {
	s.stopOnce.Do(func() {
		close(s.done)
	})
}
