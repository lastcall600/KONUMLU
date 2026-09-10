package listings

import (
	"encoding/json"
	"strconv"
	"unicode/utf8"

	mdcontracts "backend/internal/masterdata/contracts"
)

func validateAttributesAgainstForm(attrs Attributes, form mdcontracts.PublishedForm) error {
	if attrs == nil {
		return errInvalidAttributes
	}
	fields := make(map[string]mdcontracts.PublishedField, len(form.Fields))
	for _, f := range form.Fields {
		fields[f.Code] = f
	}
	for code := range attrs {
		if _, ok := fields[code]; !ok {
			return errInvalidAttributes
		}
	}
	for _, f := range form.Fields {
		val, ok := attrs[f.Code]
		if !ok {
			if f.Required {
				return errInvalidAttributes
			}
			continue
		}
		if val == nil {
			return errInvalidAttributes
		}
		if err := validateAttributeValue(f, val); err != nil {
			return err
		}
	}
	return nil
}

func validateAttributeValue(f mdcontracts.PublishedField, val any) error {
	switch f.ValueType {
	case mdcontracts.ValueTypeText:
		s, ok := val.(string)
		if !ok {
			return errInvalidAttributes
		}
		return applyTextConstraints(s, f.Constraints)
	case mdcontracts.ValueTypeInteger:
		n, ok := asJSONInteger(val)
		if !ok {
			return errInvalidAttributes
		}
		return applyNumericConstraints(float64(n), f.Constraints)
	case mdcontracts.ValueTypeDecimal:
		n, ok := asJSONNumber(val)
		if !ok {
			return errInvalidAttributes
		}
		return applyNumericConstraints(n, f.Constraints)
	case mdcontracts.ValueTypeBoolean:
		if _, ok := val.(bool); !ok {
			return errInvalidAttributes
		}
		return nil
	case mdcontracts.ValueTypeEnum:
		s, ok := val.(string)
		if !ok {
			return errInvalidAttributes
		}
		for _, code := range f.EnumOptionCodes {
			if s == code {
				return nil
			}
		}
		return errInvalidAttributes
	default:
		return errInvalidAttributes
	}
}

func applyTextConstraints(s string, c map[string]any) error {
	if c == nil {
		return nil
	}
	n := utf8.RuneCountInString(s)
	if min, ok := constraintNumber(c, "minLength", "min_length"); ok && float64(n) < min {
		return errInvalidAttributes
	}
	if max, ok := constraintNumber(c, "maxLength", "max_length"); ok && float64(n) > max {
		return errInvalidAttributes
	}
	return nil
}

func applyNumericConstraints(n float64, c map[string]any) error {
	if c == nil {
		return nil
	}
	if min, ok := constraintNumber(c, "min"); ok && n < min {
		return errInvalidAttributes
	}
	if max, ok := constraintNumber(c, "max"); ok && n > max {
		return errInvalidAttributes
	}
	return nil
}

func constraintNumber(c map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := c[k]; ok {
			return asJSONNumber(v)
		}
	}
	return 0, false
}

func asJSONInteger(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func asJSONNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
