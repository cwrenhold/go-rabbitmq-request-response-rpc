package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"server/requests"
	"server/responses"
)

// handleAddRequest is the handler for "add" type requests
func HandleAddRequest(ctx context.Context, body []byte, requestType string, receivedTime string) (interface{}, error) {
	// Parse the request body directly to an AddRequest
	var addReq requests.AddRequest
	if err := json.Unmarshal(body, &addReq); err != nil {
		return nil, fmt.Errorf("failed to unmarshal add request: %v", err)
	}

	// Calculate the sum
	sum := 0
	for _, val := range addReq.Values {
		sum += val
	}

	log.Printf("Calculated sum of %d values: %d", len(addReq.Values), sum)

	// Create and return the response with the sum
	return responses.AddResponse{
		ResponseMetadata: buildMetadata(addReq.SentAt, receivedTime),
		Sum:              sum,
	}, nil
}
