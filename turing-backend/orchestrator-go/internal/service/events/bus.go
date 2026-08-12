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

func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
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
