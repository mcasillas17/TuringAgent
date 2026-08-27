package repository

import (
	"context"
	"sync"
)

// keyedLockTable serialises work that is about one named thing.
//
// Process-wide on purpose, wherever it is used: two Repository values over one
// database must contend on the same key, or the serialisation is only as good
// as the number of callers that happen to share an instance. Entries are
// reference-counted and dropped when nobody holds or wants them, so a table
// keyed on identifiers that keep being minted does not grow without bound.
//
// Acquisition honours the caller's context, so a request that has ended stops
// waiting rather than queueing behind work it can no longer receive.
type keyedLockTable struct {
	mutex sync.Mutex
	locks map[string]*keyedLockEntry
}

type keyedLockEntry struct {
	token chan struct{}
	refs  int
}

func newKeyedLockTable() *keyedLockTable {
	return &keyedLockTable{locks: make(map[string]*keyedLockEntry)}
}

func (t *keyedLockTable) lockContext(ctx context.Context, key string) (func(), error) {
	t.mutex.Lock()
	entry := t.locks[key]
	if entry == nil {
		entry = &keyedLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		t.locks[key] = entry
	}
	entry.refs++
	t.mutex.Unlock()

	releaseReference := func() {
		t.mutex.Lock()
		entry.refs--
		if entry.refs == 0 && t.locks[key] == entry {
			delete(t.locks, key)
		}
		t.mutex.Unlock()
	}
	if err := ctx.Err(); err != nil {
		releaseReference()
		return nil, err
	}
	select {
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			releaseReference()
			return nil, err
		}
		var once sync.Once
		return func() {
			once.Do(func() {
				entry.token <- struct{}{}
				releaseReference()
			})
		}, nil
	case <-ctx.Done():
		releaseReference()
		return nil, ctx.Err()
	}
}
