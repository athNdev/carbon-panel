package module

import "testing"

// MINE-127: pins the extracted compareConditionValues helper — numeric
// operators, case-insensitive string equality, unsupported-operator signal.
func TestCompareConditionValues(t *testing.T) {
	cases := []struct {
		actual, operator, expected string
		want, supported            bool
	}{
		{"5", ">", "3", true, true},
		{"3", ">", "5", false, true},
		{"5", "==", "5.0", true, true},
		{"5", "!=", "6", true, true},
		{"5", "<=", "5", true, true},
		{"4", ">=", "5", false, true},
		{"Running", "==", "running", true, true},
		{"Running", "!=", "stopped", true, true},
		{"a", ">", "b", false, false},
		{"5", "===", "5", false, false},
	}
	for _, c := range cases {
		got, supported := compareConditionValues(c.actual, c.operator, c.expected)
		if got != c.want || supported != c.supported {
			t.Errorf("compareConditionValues(%q,%q,%q) = (%v,%v), want (%v,%v)",
				c.actual, c.operator, c.expected, got, supported, c.want, c.supported)
		}
	}
}
