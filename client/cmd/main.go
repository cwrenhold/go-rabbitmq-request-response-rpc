package main

import (
	"log"
	"net/http"
	"os"

	"client/handlers"
	"client/rpc"
)

func main() {
	port := os.Getenv("CLIENT_PORT")
	if port == "" {
		port = "8080" // Default to 8080 if CLIENT_PORT is not set
	}

	// Initialize RPC client
	rpcClient, err := rpc.NewRPCClient()
	if err != nil {
		log.Fatalf("Failed to initialize RPC client: %v", err)
	}
	defer rpcClient.Close()

	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		handlers.HelloHandler(w, r, rpcClient)
	})

	http.HandleFunc("/hello_sql", func(w http.ResponseWriter, r *http.Request) {
		handlers.HelloSQLHandler(w, r, rpcClient)
	})

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		handlers.AddHandler(w, r, rpcClient)
	})

	log.Printf("Starting HTTP server on port %s", port)
	http.ListenAndServe(":"+port, nil)
}
