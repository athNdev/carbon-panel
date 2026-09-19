package module

// Typed condition comparators for module event-hook conditions.
//
// Kept in the module package (not a shared pkg) deliberately: single caller
// (Manager.compareValues in hooks.go) and string-comparison fallback needs the
// manager logger for unsupported-operator warnings. A shared pkg would either
// drag a logger dependency along or silently swallow that warning.
func compareNumeric(actualNum, expectedNum float64, operator string) (bool, bool) {
	switch operator {
	case "==":
		return actualNum == expectedNum, true
	case "!=":
		return actualNum != expectedNum, true
	case "<":
		return actualNum < expectedNum, true
	case ">":
		return actualNum > expectedNum, true
	case "<=":
		return actualNum <= expectedNum, true
	case ">=":
		return actualNum >= expectedNum, true
	}
	return false, false
}

func compareStrings(actual, expected, operator string) (bool, bool) {
	switch operator {
	case "==":
		return actual == expected, true
	case "!=":
		return actual != expected, true
	}
	return false, false
}
