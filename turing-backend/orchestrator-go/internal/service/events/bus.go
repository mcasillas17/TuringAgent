package events

import "sync"

const maxTerminatedSessionFences = 1024

// AttentionChangedType is the Needs-you queue's hint: something it lists may
// have changed, so read ListAttention again. It carries nothing else.
const AttentionChangedType = "attention.changed"

// changesAttention reports whether an event is a transition of a source the
// Needs-you queue lists: an approval, a delegated task's consent (reported by
// its delegation events), an automation's outcome (a run ending; the bus
// cannot tell an automation's run from a chat's, so every ending hints), or a
// deletion.
func changesAttention(eventType string) bool {
	switch eventType {
	case "approval.requested", "approval.approved", "approval.denied", "approval.expired", "approval.consumed",
		"delegation.started", "delegation.updated", "delegation.finished",
		"agent.run.completed", "agent.run.failed", "agent.run.cancelled",
		"session.deleted":
		return true
	default:
		return false
	}
}

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

// PublishAttentionChanged hints every updates subscriber that the Needs-you
// queue may have changed, for a transition that publishes no event of its
// own: a remote-egress block, an automation enabled, disabled, edited or
// deleted, a dismissal, or a deletion receipt that failed.
func (b *Bus) PublishAttentionChanged() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		if sub.updates != nil {
			sub.updates.hintAttention()
		}
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
	mu      sync.Mutex
	pending map[string]Event
	// attention is one waiting attention.changed hint. Hints coalesce, so a
	// burst of changes is one hint, not one per change.
	attention bool
	wake      chan struct{}
	out       chan Event
	done      chan struct{}
	stopOnce  sync.Once
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
	session := event.Type == "session.updated" || event.Type == "session.deleted"
	attention := changesAttention(event.Type)
	if !session && !attention {
		return
	}
	s.mu.Lock()
	if session {
		previous, found := s.pending[event.SessionID]
		if !found || event.Sequence >= previous.Sequence {
			s.pending[event.SessionID] = event
		}
	}
	s.attention = s.attention || attention
	s.mu.Unlock()
	s.signal()
}

func (s *sessionUpdateSubscription) hintAttention() {
	s.mu.Lock()
	s.attention = true
	s.mu.Unlock()
	s.signal()
}

func (s *sessionUpdateSubscription) signal() {
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
				// The hint goes after every waiting session event, so a
				// client refetching on it already holds the deletions.
				if next.SessionID == "" && s.attention {
					next, s.attention = Event{Type: AttentionChangedType}, false
				}
				s.mu.Unlock()
				if next.Type == "" {
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
