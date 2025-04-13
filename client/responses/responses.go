package responses

type ResponseMetadata struct {
	SentAt      string `json:"sentAt"`
	ReceivedAt  string `json:"receivedAt"`
	RespondedAt string `json:"respondedAt"`
}

type AddResponse struct {
	ResponseMetadata
	Sum int `json:"sum"`
}
