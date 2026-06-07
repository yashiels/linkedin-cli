package types

// JobAlert represents a saved LinkedIn job alert subscription.
type JobAlert struct {
	// ID is the numeric alert identifier (from the URN).
	ID string `json:"id"`
	// URN is the full LinkedIn URN, e.g. "urn:li:jobAlert:123".
	URN      string `json:"urn"`
	Keywords string `json:"keywords"`
	Location string `json:"location"`
	// Frequency is how often emails are sent: "DAILY", "WEEKLY", etc.
	Frequency string `json:"frequency"`
	// CreatedAt is a human-readable creation date string.
	CreatedAt string `json:"createdAt"`
}
