package attention

import (
	"reflect"
	"testing"
)

// The closed table, written out independently of the implementation: each
// stage in order with the conditions that raise it.
var wantTable = []struct {
	stage      StageID
	conditions []string
}{
	{PersonApproval, []string{"PendingApproval"}},
	{PersonConsent, []string{"PendingConsent"}},
	{PersonBlocked, []string{"BlockedAutomation", "SuspendedWatch"}},
	{PersonDeletion, []string{"RetryableDeletion"}},
	{PersonProposal, []string{"OpenProposal"}},
}

// Every condition is a bool field of Conditions and belongs to exactly one
// stage, so adding a condition without classifying it fails here.
func TestEveryConditionBelongsToExactlyOneStage(t *testing.T) {
	fields := reflect.TypeFor[Conditions]()
	seen := map[string]int{}
	for _, row := range wantTable {
		for _, name := range row.conditions {
			seen[name]++
		}
	}
	for index := range fields.NumField() {
		field := fields.Field(index)
		if field.Type.Kind() != reflect.Bool {
			t.Fatalf("Conditions.%s is %s, want bool", field.Name, field.Type)
		}
		if seen[field.Name] != 1 {
			t.Fatalf("Conditions.%s is in %d stages, want exactly 1", field.Name, seen[field.Name])
		}
		delete(seen, field.Name)
	}
	if len(seen) != 0 {
		t.Fatalf("the table names conditions Conditions does not have: %v", seen)
	}
}

// Every combination of qualifying conditions resolves to exactly one stage:
// the first in the table's order whose condition holds, or unavailable when
// none does.
func TestStageResolvesEveryCombinationToTheFirstMatch(t *testing.T) {
	fields := reflect.TypeFor[Conditions]()
	combinations := 1 << fields.NumField()
	counts := map[StageID]int{}
	for mask := range combinations {
		var conditions Conditions
		value := reflect.ValueOf(&conditions).Elem()
		held := map[string]bool{}
		for index := range fields.NumField() {
			if mask&(1<<index) != 0 {
				value.Field(index).SetBool(true)
				held[fields.Field(index).Name] = true
			}
		}
		want := Unavailable
	table:
		for _, row := range wantTable {
			for _, name := range row.conditions {
				if held[name] {
					want = row.stage
					break table
				}
			}
		}
		got := Stage(conditions)
		if got != want {
			t.Fatalf("Stage(%+v) = %q, want %q", conditions, got, want)
		}
		counts[got]++
	}
	if len(counts) != len(wantTable)+1 {
		t.Fatalf("stages reached = %v over %d combinations, want every stage and unavailable", counts, combinations)
	}
}

// The stage rank is the table's Order column; unavailable has none of its
// own, because an unclassified row keeps its source's place.
func TestRanksFollowTheTablesOrder(t *testing.T) {
	for index, row := range wantTable {
		if got := Rank(row.stage); got != index+1 {
			t.Fatalf("Rank(%s) = %d, want %d", row.stage, got, index+1)
		}
	}
	if got := Rank(Unavailable); got != 0 {
		t.Fatalf("Rank(unavailable) = %d, want 0", got)
	}
}

// Each source sits in one stage, and the kinds sort in the fixed order
// approval, delegation, automation, deletion, watch, proposal.
func TestEachSourceKindSitsInOneStage(t *testing.T) {
	want := []struct {
		kind  Kind
		stage StageID
	}{
		{KindApproval, PersonApproval},
		{KindDelegation, PersonConsent},
		{KindAutomation, PersonBlocked},
		{KindDeletion, PersonDeletion},
		{KindWatch, PersonBlocked},
		{KindProposal, PersonProposal},
	}
	for index, row := range want {
		if int(row.kind) != index+1 {
			t.Fatalf("kind %d sorts at %d, want %d", row.kind, row.kind, index+1)
		}
		if got := SourceStage(row.kind); got != row.stage {
			t.Fatalf("SourceStage(%d) = %s, want %s", row.kind, got, row.stage)
		}
	}
	for _, unknown := range []Kind{0, KindProposal + 1} {
		if got := SourceStage(unknown); got != Unavailable {
			t.Fatalf("SourceStage(%d) = %s, want unavailable", unknown, got)
		}
	}
}
