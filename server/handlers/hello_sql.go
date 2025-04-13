package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"server/requests"
)

// handleHelloRequest is the handler for "hello" type requests
func HandleHelloSqlRequest(ctx context.Context, body []byte, requestType string, receivedTime string) (interface{}, error) {
	// Parse the request body
	var req requests.RequestMetadata
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hello sql request: %v", err)
	}

	// Build the connection string from environment variables
	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_HOSTNAME"),
		os.Getenv("POSTGRES_PORT"),
		os.Getenv("POSTGRES_DB"),
	)

	// Connect to the database
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %v", err)
	}
	defer conn.Close(ctx)

	// Execute the query to check the connection
	var result int
	err = conn.QueryRow(ctx, "SELECT 1 LIMIT 1").Scan(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %v", err)
	}

	// Return success response
	return buildMetadata(req.SentAt, receivedTime), nil
}
