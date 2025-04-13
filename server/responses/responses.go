package responses

// ResponseMetadata represents the structure of our RPC response
type ResponseMetadata struct {
	SentAt      string `json:"sentAt"`
	ReceivedAt  string `json:"receivedAt"`
	RespondedAt string `json:"respondedAt"`
}

// AddResponse extends the base Response for the "add" endpoint
type AddResponse struct {
	ResponseMetadata
	Sum int `json:"sum"`
}
