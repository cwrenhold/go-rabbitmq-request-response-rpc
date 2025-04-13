package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"client/responses"
	"client/rpc"
)

func handleRPCRequest(
	w http.ResponseWriter,
	r *http.Request,
	rpcClient *rpc.RPCClient,
	requestType string,
	requestCreator func() (interface{}, error),
	responseProcessor func([]byte) ([]byte, error),
) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := requestCreator()
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	jsonRequest, err := json.Marshal(req)
	if err != nil {
		log.Printf("Failed to marshal request to JSON: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response, err := rpcClient.Call(ctx, requestType, jsonRequest)
	if err != nil {
		log.Printf("RPC call failed: %v", err)
		http.Error(w, "RPC call failed", http.StatusInternalServerError)
		return
	}

	processedResponse, err := responseProcessor(response)
	if err != nil {
		log.Printf("Failed to process response: %v", err)
		http.Error(w, "Failed to parse response", http.StatusInternalServerError)
		return
	}

	// Example usage of responses package
	currentTime := time.Now().Format(time.RFC3339)
	responseMetadata := responses.ResponseMetadata{
		SentAt:      currentTime,
		ReceivedAt:  currentTime,
		RespondedAt: currentTime,
	}
	log.Printf("Response metadata: %+v", responseMetadata)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(processedResponse)
}
