package netgsm

import "testing"

func TestNormalizeTRMobile(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"+905551112233", "5551112233", true},
		{"905551112233", "5551112233", true},
		{"05551112233", "5551112233", true},
		{"5551112233", "5551112233", true},
		{" +90 555 111 22 33 ", "5551112233", true},
		{"+15551234567", "", false},
		{"2125556677", "", false},
		{"", "", false},
		{"not-a-phone", "", false},
		{"+90", "", false},
		{"555111223", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeTRMobile(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("in=%q got=%q ok=%v want=%q ok=%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
