package requests

type RequestMetadata struct {
	SentAt string `json:"sentAt"`
}

type AddRequest struct {
	RequestMetadata
	Values []int `json:"values"`
}
