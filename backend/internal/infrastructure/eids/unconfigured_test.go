package eidsadapter

import (
	"context"
	"testing"

	"backend/internal/eids"
)

func TestUnconfiguredNeverVerifies(t *testing.T) {
	g := Unconfigured{}
	prop, err := g.VerifyProperty(context.Background(), eids.PropertyVerifyRequest{})
	if err != nil || prop.Outcome != eids.OutcomeUnavailable {
		t.Fatalf("property = %+v err=%v", prop, err)
	}
	veh, err := g.VerifyVehicle(context.Background(), eids.VehicleVerifyRequest{})
	if err != nil || veh.Outcome != eids.OutcomeUnavailable {
		t.Fatalf("vehicle = %+v err=%v", veh, err)
	}
}
