package services

// Bounds applied to client-supplied list limits. A negative or zero limit is
// treated as "use the default" because GORM interprets a negative Limit as
// "no limit", which would let a caller dump an entire table in one response.
const (
	defaultListLimit = 50
	maxListLimit     = 500
)

// clampLimit normalises a client-supplied list limit into [1, maxListLimit].
func clampLimit(requested int) int {
	switch {
	case requested <= 0:
		return defaultListLimit
	case requested > maxListLimit:
		return maxListLimit
	default:
		return requested
	}
}
