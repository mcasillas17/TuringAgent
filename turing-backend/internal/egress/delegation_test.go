package egress

import "testing"

// The orchestrator's caps on specialist results are sized against 64 KiB, so a
// join's framing has to admit exactly that much rather than the default.
func TestDelegationResultsFramingIsBoundedAt64KiB(t *testing.T) {
	framing := DelegationResultsFraming()
	if framing.MaxBytes != 64*1024 || DelegationResultsFrameMaxBytes != 64*1024 {
		t.Fatalf("MaxBytes = %d, constant = %d, want both %d", framing.MaxBytes, DelegationResultsFrameMaxBytes, 64*1024)
	}
	if err := framing.validate(); err != nil {
		t.Fatalf("framing is invalid: %v", err)
	}
}
