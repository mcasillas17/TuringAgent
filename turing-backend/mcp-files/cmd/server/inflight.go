package main

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"sync"
)

// inflightRegistry tracks the requests this server is currently working on so
// that a `notifications/cancelled` can stop the matching one.
//
// Entries are keyed by the *authenticated* caller as well as the JSON-RPC
// request id: the id is chosen by whoever sent the request, so on its own it
// would let any authenticated caller cancel any other caller's work. Today the
// bundled bearer resolves to a single agent identity, but the key is built from
// whatever the authenticator returned rather than from that assumption.
//
// The map is bounded. Each entry is one HTTP request currently executing, so
// the bound is a ceiling on concurrent cancellable work; past it a request is
// still served, it simply cannot be cancelled by id — refusing the work
// outright would turn a cancellation feature into an availability limit.
type inflightRegistry struct {
	mu      sync.Mutex
	cancels map[string]inflightEntry
	limit   int
	nextSeq uint64
}

// inflightEntry pairs the cancel with a sequence number. The sequence is what
// makes a late release safe: two requests can legitimately reuse one id one
// after the other, and Go function values cannot be compared, so identity has
// to be carried explicitly rather than derived from the closure.
type inflightEntry struct {
	cancel context.CancelFunc
	seq    uint64
}

func newInflightRegistry(limit int) *inflightRegistry {
	return &inflightRegistry{cancels: make(map[string]inflightEntry), limit: limit}
}

// add registers cancel under (agentID, id) and returns the release to call when
// the request finishes. It reports false when the id is already in flight for
// this caller, or when the registry is full; the caller then runs without being
// cancellable rather than failing.
func (r *inflightRegistry) add(agentID string, id any, cancel context.CancelFunc) (release func(), ok bool) {
	key, keyed := inflightKey(agentID, id)
	if !keyed {
		return func() {}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, present := r.cancels[key]; present {
		return func() {}, false
	}
	if len(r.cancels) >= r.limit {
		return func() {}, false
	}
	r.nextSeq++
	seq := r.nextSeq
	r.cancels[key] = inflightEntry{cancel: cancel, seq: seq}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		// Check the sequence before deleting: a later request may already have
		// taken this key, and a late or duplicate release must not make that
		// one uncancellable.
		if current, present := r.cancels[key]; present && current.seq == seq {
			delete(r.cancels, key)
		}
	}, true
}

// cancel stops the request the caller named, and reports whether there was one.
// An unknown id is not an error: the specification says a receiver may ignore a
// cancellation whose request is unknown or already finished.
func (r *inflightRegistry) cancel(agentID string, id any) bool {
	key, keyed := inflightKey(agentID, id)
	if !keyed {
		return false
	}
	r.mu.Lock()
	entry, present := r.cancels[key]
	if present {
		delete(r.cancels, key)
	}
	r.mu.Unlock()
	if !present {
		return false
	}
	entry.cancel()
	return true
}

// inflightKey builds the lookup key, keeping the id's JSON type: a string id
// "1" and a numeric id 1 are different requests, and collapsing them would let
// one be cancelled in place of the other.
//
// A numeric id reaches this function as two different Go types depending on
// where it came from, and both have to land on the same key. A *request* id is
// decoded with UseNumber (jsonrpc.decodeID) and arrives as a json.Number; the
// requestId inside a cancellation's params is decoded without it and arrives as
// a float64. Handling only json.Number would make cancellation silently inert
// for exactly the ids the agent runtime sends, which are always integers.
//
// The float form is canonicalized to the same decimal text json.Number carries,
// and the one spelling the two forms disagree on — "-0", which jsonrpc's id
// grammar accepts but strconv renders as "0" — is folded to the positive form
// on both sides. Beyond that the mapping is exact for every id the protocol
// permits here: jsonrpc.decodeID accepts only plain decimal integers (no
// exponent, no fraction), so a non-integral or out-of-range float can never
// name a registered request and is refused rather than rounded onto one.
func inflightKey(agentID string, id any) (string, bool) {
	switch typed := id.(type) {
	case string:
		return agentID + "\x00s\x00" + typed, true
	case json.Number:
		text := typed.String()
		if text == "-0" {
			// JSON's -0 and 0 are one number, and the float form below always
			// canonicalizes to "0". Folding the sign here keeps both spellings
			// on one key: otherwise a request registered as "-0" could never be
			// cancelled, and a cancellation naming -0 would reach whichever
			// sibling request holds the id 0.
			text = "0"
		}
		return agentID + "\x00n\x00" + text, true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) {
			return "", false
		}
		if typed < minSafeJSONInteger || typed > maxSafeJSONInteger {
			return "", false
		}
		return agentID + "\x00n\x00" + strconv.FormatInt(int64(typed), 10), true
	default:
		return "", false
	}
}

// minSafeJSONInteger and maxSafeJSONInteger bound the integers a float64 still
// represents exactly. Outside that range two distinct ids share one float, so a
// cancellation could name a request it does not own.
const (
	maxSafeJSONInteger = float64(1<<53 - 1)
	minSafeJSONInteger = -maxSafeJSONInteger
)
