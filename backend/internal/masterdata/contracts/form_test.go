package contracts

import "testing"

func TestPublishedFormDTOIsMinimal(t *testing.T) {
	var id ID
	id[0] = 1
	form := PublishedForm{
		CategoryID:    id,
		SchemaVersion: 2,
		Fields: []PublishedField{{
			Code:            "condition",
			ValueType:       ValueTypeEnum,
			Required:        true,
			Constraints:     map[string]any{"min": 1},
			EnumOptionCodes: []string{"used"},
		}},
	}
	if form.CategoryID.IsZero() || form.SchemaVersion != 2 {
		t.Fatalf("form = %+v", form)
	}
	if form.Fields[0].Code != "condition" || form.Fields[0].ValueType != ValueTypeEnum {
		t.Fatalf("field = %+v", form.Fields[0])
	}
	if len(form.Fields[0].EnumOptionCodes) != 1 || form.Fields[0].EnumOptionCodes[0] != "used" {
		t.Fatalf("options = %+v", form.Fields[0].EnumOptionCodes)
	}
}
