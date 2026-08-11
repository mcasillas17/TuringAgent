// Package memory recalls relevant messages from a user's EARLIER sessions and
// renders them for inclusion in a model request.
//
// Scope: this package only re-surfaces messages that were actually said, each
// carrying its own provenance. It stores nothing and derives no facts, so it has
// no supersession or staleness problem. Persistent facts about the user are a
// separate concern and deliberately not built here.
//
// # Wiring (deferred)
//
// Recall is dormant until the agent calls it. Once the tool-calling loop lands,
// prepend the block to the request messages in general_assistant.go:
//
//	if block, ok := a.recall.Recall(ctx, job.GetSessionId(), job.GetUserText()); ok {
//		requestMessages = append([]llm.ChatMessage{block}, requestMessages...)
//	}
//
// It is prepended rather than appended so recalled material sits before the
// live conversation and cannot be mistaken for the user's latest turn.
package memory

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
)

const (
	maxTerms    = 6
	minTermLen  = 3
	perTermHits = 10
	ellipsis    = "…"
)

// stopwords are dropped before searching. Deliberately small: an aggressive list
// would silently discard terms that carry the user's actual intent.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "was": true, "were": true, "with": true,
	"that": true, "this": true, "you": true, "your": true, "did": true, "does": true,
	"how": true, "what": true, "when": true, "where": true, "why": true, "who": true,
	"can": true, "are": true, "his": true, "her": true, "our": true, "its": true,
	"but": true, "not": true, "from": true, "into": true, "about": true, "have": true,
	"has": true, "had": true, "will": true, "would": true, "should": true, "could": true,
}

// Excerpt is one recalled message. Provenance travels with the content so the
// rendered block can attribute and date every line shown to the model.
type Excerpt struct {
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

// Searcher is the orchestrator lookup this package needs, kept narrow so tests
// can supply a fake without standing up a gRPC server.
type Searcher interface {
	SearchMessages(ctx context.Context, query string, limit int) ([]Excerpt, error)
}

// Recaller surfaces excerpts from earlier sessions.
type Recaller struct {
	Search      Searcher
	MaxExcerpts int
	MaxChars    int
	Timeout     time.Duration
}

// Recall returns a system message of relevant excerpts from earlier sessions, or
// ok=false when there is nothing worth adding.
//
// It never returns an error. Recall is an enhancement, and a backend that is
// down, slow, or empty must degrade to "no block" rather than fail the turn.
func (r *Recaller) Recall(ctx context.Context, currentSessionID string, userText string) (llm.ChatMessage, bool) {
	if r == nil || r.Search == nil {
		return llm.ChatMessage{}, false
	}
	queries := terms(userText)
	if len(queries) == 0 {
		return llm.ChatMessage{}, false
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	// SearchMessages is an exact PHRASE search: the orchestrator wraps whatever
	// it is given in double quotes, so a whole utterance would match nothing and
	// FTS5 OR/AND operators cannot be injected. One single-term query each (a
	// one-word phrase is a plain term match) and merge here instead. The store is
	// a local SQLite file over loopback, so the extra round-trips are cheap.
	hits := make(map[string][]Excerpt, len(queries))
	for _, query := range queries {
		found, err := r.Search.SearchMessages(ctx, query, perTermHits)
		if err != nil {
			return llm.ChatMessage{}, false
		}
		hits[query] = found
	}

	excerpts := rank(hits, currentSessionID, r.MaxExcerpts, r.MaxChars)
	return Render(excerpts)
}

// terms reduces the user's text to search terms: lowercased, stopwords and very
// short words dropped, deduped, order preserved, capped.
func terms(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !isWordRune(r)
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, maxTerms)
	for _, field := range fields {
		if len(field) < minTermLen || stopwords[field] || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
		if len(out) == maxTerms {
			break
		}
	}
	return out
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

type scored struct {
	excerpt Excerpt
	matches int
	order   int
}

// rank merges per-term hits into one ordered, budgeted list: most distinct terms
// matched first, most recent breaking ties.
func rank(hits map[string][]Excerpt, currentSessionID string, maxExcerpts int, maxChars int) []Excerpt {
	byKey := map[string]*scored{}
	next := 0
	// Iterate terms in a stable order so the result does not depend on Go's
	// randomised map iteration.
	termKeys := make([]string, 0, len(hits))
	for term := range hits {
		termKeys = append(termKeys, term)
	}
	sort.Strings(termKeys)

	for _, term := range termKeys {
		for _, excerpt := range hits[term] {
			// The current session's history is already in the request; recalling
			// it would duplicate context and spend budget for nothing.
			if excerpt.SessionID == currentSessionID {
				continue
			}
			if excerpt.Role != "user" && excerpt.Role != "assistant" {
				continue
			}
			if strings.TrimSpace(excerpt.Content) == "" {
				continue
			}
			key := excerpt.SessionID + "|" + excerpt.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + excerpt.Content
			if existing, ok := byKey[key]; ok {
				existing.matches++
				continue
			}
			byKey[key] = &scored{excerpt: excerpt, matches: 1, order: next}
			next++
		}
	}

	ordered := make([]*scored, 0, len(byKey))
	for _, entry := range byKey {
		ordered = append(ordered, entry)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].matches != ordered[j].matches {
			return ordered[i].matches > ordered[j].matches
		}
		if !ordered[i].excerpt.CreatedAt.Equal(ordered[j].excerpt.CreatedAt) {
			return ordered[i].excerpt.CreatedAt.After(ordered[j].excerpt.CreatedAt)
		}
		return ordered[i].order < ordered[j].order
	})

	out := make([]Excerpt, 0, len(ordered))
	remaining := maxChars
	for _, entry := range ordered {
		if maxExcerpts > 0 && len(out) >= maxExcerpts {
			break
		}
		if remaining <= 0 {
			break
		}
		excerpt := entry.excerpt
		// Truncate rather than drop: a clipped excerpt still carries its date and
		// gist, and the ellipsis tells the model it is seeing part of something.
		if len(excerpt.Content) > remaining {
			excerpt.Content = truncate(excerpt.Content, remaining)
		}
		remaining -= len(excerpt.Content)
		out = append(out, excerpt)
	}
	return out
}

func truncate(content string, limit int) string {
	if limit <= len(ellipsis) {
		return ellipsis
	}
	runes := []rune(content)
	keep := limit - len(ellipsis)
	if keep > len(runes) {
		keep = len(runes)
	}
	// Trim back to a whole rune budget, then to a word boundary when one is near.
	clipped := strings.TrimRight(string(runes[:keep]), " ")
	for len(clipped) > limit-len(ellipsis) {
		clipped = clipped[:len(clipped)-1]
	}
	return clipped + ellipsis
}

// Render turns excerpts into a single system message.
//
// Recalled material never enters as a conversation turn: it is framed as clearly
// separate, older, and possibly stale. Blending it into the live transcript is
// how a model comes to believe the user just said something they said weeks ago.
// Dates are absolute because this block may be re-read in a session far later.
func Render(excerpts []Excerpt) (llm.ChatMessage, bool) {
	if len(excerpts) == 0 {
		return llm.ChatMessage{}, false
	}
	var builder strings.Builder
	builder.WriteString("The following are excerpts from EARLIER conversations with this user, ")
	builder.WriteString("retrieved because they may be relevant. They are NOT part of the current ")
	builder.WriteString("conversation and may be out of date. Cite the date if you rely on one.\n\n")
	for _, excerpt := range excerpts {
		builder.WriteString("[")
		builder.WriteString(excerpt.CreatedAt.UTC().Format("2006-01-02"))
		builder.WriteString("] ")
		builder.WriteString(excerpt.Role)
		builder.WriteString(": ")
		builder.WriteString(excerpt.Content)
		builder.WriteString("\n")
	}
	return llm.ChatMessage{Role: "system", Content: builder.String()}, true
}
