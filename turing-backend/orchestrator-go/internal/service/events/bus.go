package events

import "sync"

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
	mu         sync.Mutex
	bufferSize int
	nextID     int64
	subs       map[int64]subscription
}

type subscription struct {
	sessionID string
	ch        chan Event
	updates   *sessionUpdateSubscription
}

func NewBus(bufferSize int) *Bus {
	return &Bus{bufferSize: bufferSize, subs: map[int64]subscription{}}
}

func (b *Bus) Subscribe(sessionID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan Event, b.bufferSize)
	b.subs[id] = subscription{sessionID: sessionID, ch: ch}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		sub, ok := b.subs[id]
		if !ok {
			return
		}
		delete(b.subs, id)
		close(sub.ch)
	}
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
	for _, sub := range b.subs {
		if sub.updates != nil {
			sub.updates.publish(event)
			continue
		}
		if sub.sessionID != event.SessionID {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			latest := event
			draining := true
			for draining {
				select {
				case queued := <-sub.ch:
					if queued.Sequence >= latest.Sequence {
						latest = queued
					}
				default:
					draining = false
				}
			}
			select {
			case sub.ch <- latest:
			default:
			}
		}
	}
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
	if event.Type != "session.updated" {
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
