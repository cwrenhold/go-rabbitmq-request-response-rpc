package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"server/requests"
)

// handleHelloRequest is the handler for "hello" type requests
func HandleHelloRequest(ctx context.Context, body []byte, requestType string, receivedTime string) (interface{}, error) {
	// Parse the request body
	var req requests.RequestMetadata
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hello request: %v", err)
	}

	// The hello handler simply echoes back with timestamps
	return buildMetadata(req.SentAt, receivedTime), nil
}
