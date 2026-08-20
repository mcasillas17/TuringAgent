// Package constructorscan is scanner input, not a build target. Go tooling
// ignores testdata, so nothing here compiles into the module; it exists so the
// constructor scan can be tested against the shapes it must catch instead of
// only against the one shape this package happens to declare today.
//
// The forbidden constructor the real assertion guards is
// UserCancelledCancellation, and a future author would most likely write it
// returning a pointer. That is the shape pinned here.
package constructorscan

type Cancellation struct{}

type StepNotice struct{}

// UserCancelledCancellation is the exact future addition the scan must catch.
func UserCancelledCancellation() *Cancellation { return &Cancellation{} }

func ValueCancellation() Cancellation { return Cancellation{} }

func WrappedCancellation() (*Cancellation, error) { return &Cancellation{}, nil }

// PairedCancellations returns the type twice; the scan must name it once.
func PairedCancellations() (Cancellation, *Cancellation) { return Cancellation{}, &Cancellation{} }

func unexportedCancellation() *Cancellation { return &Cancellation{} }

func NoticeConstructor() *StepNotice { return &StepNotice{} }

func NoCancellationAtAll() error { return nil }

// Clone is a method, not a constructor, so the scan must skip it.
func (c *Cancellation) Clone() *Cancellation { return c }
