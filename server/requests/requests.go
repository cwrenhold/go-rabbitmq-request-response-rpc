package requests

// Base request and response types
type RequestMetadata struct {
	SentAt string `json:"sentAt"`
}

// AddRequest extends the base Request for the "add" endpoint
type AddRequest struct {
	RequestMetadata
	Values []int `json:"values"`
}
