package llm

import "context"

func sendStreamEvent(ctx context.Context, out chan<- StreamEvent, event StreamEvent) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
