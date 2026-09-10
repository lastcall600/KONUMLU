package contracts

import "testing"

func TestEIDSRequirementKeepsPropertyAndVehicleSeparate(t *testing.T) {
	if EIDSRequirementNone.Required() {
		t.Fatal("none must not require EİDS")
	}
	if !EIDSRequirementProperty.Required() || !EIDSRequirementVehicle.Required() {
		t.Fatal("property and vehicle must require EİDS")
	}
	if EIDSRequirementProperty == EIDSRequirementVehicle {
		t.Fatal("property and vehicle must stay distinct")
	}
	if EIDSRequirement("identity").Valid() {
		t.Fatal("person identity is not a listing EİDS requirement")
	}
}
