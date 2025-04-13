package main

import (
	"server/handlers"
	"server/rpc"
)

// NewRequestRouter creates a new router with default handlers
func NewRequestRouter() *rpc.RequestRouter {
	router := &rpc.RequestRouter{
		Handlers: make(map[string]rpc.RequestHandler),
	}

	// Register the default hello handler
	router.RegisterHandler("hello", handlers.HandleHelloRequest)

	// Register the add handler
	router.RegisterHandler("add", handlers.HandleAddRequest)

	return router
}

func main() {
	// Create a request router
	router := NewRequestRouter()

	// Handle RPC requests using the router
	rpc.HandleRPCRequests(router)
}
