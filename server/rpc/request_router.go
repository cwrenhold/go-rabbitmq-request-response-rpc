package rpc

import (
	"context"
	"fmt"
)

// RequestHandler is a function type for request handlers that receives raw message body
type RequestHandler func(context.Context, []byte, string, string) (interface{}, error)

// RequestRouter routes requests to appropriate handlers
type RequestRouter struct {
	Handlers map[string]RequestHandler
}

// RegisterHandler adds a new handler for a specific request type
func (r *RequestRouter) RegisterHandler(requestType string, handler RequestHandler) {
	r.Handlers[requestType] = handler
}

// RouteRequest routes a request to the appropriate handler
func (r *RequestRouter) RouteRequest(ctx context.Context, requestType string, body []byte, receivedTime string) (interface{}, error) {
	handler, exists := r.Handlers[requestType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for request type: %s", requestType)
	}

	return handler(ctx, body, requestType, receivedTime)
}
