package contracts

import (
	"errors"
	"testing"
)

func TestExecutionIntentValidate(t *testing.T) {
	var id ID
	id[0] = 1
	intent := ExecutionIntent{
		ActionID: id, CaseID: id, TargetID: id,
		TargetType: "listing", ActionType: "remove",
	}
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ExecutionIntent{}).Validate(); !errors.Is(err, ErrZeroID) {
		t.Fatalf("zero err = %v", err)
	}
	bad := intent
	bad.ActionType = ""
	if err := bad.Validate(); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("empty type err = %v", err)
	}
}
