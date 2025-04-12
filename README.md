# Example RabbitMQ RPC Service with Go

A demonstration of RPC (Remote Procedure Call) pattern using RabbitMQ in Go.

## Project Overview

This project consists of:

- A **Client** service that exposes an HTTP endpoint and makes RPC calls to RabbitMQ
- A **Server** service that processes RPC requests from RabbitMQ and sends back responses

The project showcases RabbitMQ best practices for implementing the RPC pattern, including:
- Using correlation IDs to match responses with requests
- Single callback queues for improved efficiency
- Request type routing for handling different kinds of requests
- ISO 8601 timestamps for tracking request/response timing

## Prerequisites

- Docker and Docker Compose
- VS Code with Remote Containers extension

## Running the Project

The project is set up to run in a development container with all necessary dependencies:

1. **Open the project in VS Code**:
   ```bash
   code /path/to/project
   ```

2. **Reopen in Container**: 
   When prompted by VS Code, click "Reopen in Container" or use the command palette (F1) and select "Remote-Containers: Reopen in Container"

3. **Start the server**:
   ```bash
   cd server
   go run cmd/main.go
   ```

4. **In another terminal, start the client**:
   ```bash
   cd client
   go run cmd/main.go
   ```

5. **Test the endpoint**:
   ```bash
   curl http://localhost:8080/hello
   ```
   
   You should receive a JSON response with the request and response timestamps.

## Using VS Code Debugging

The project includes launch configurations for both client and server:

1. Open the Debug panel in VS Code
2. Select "Launch Server" or "Launch Client" from the dropdown
3. Press F5 to start debugging

## Environment Variables

Environment variables are configured in `.devcontainer/.env`:

- `CLIENT_PORT`: HTTP port for the client service (default: 8080)
- `RABBITMQ_*`: Connection parameters for RabbitMQ
- `POSTGRES_*`: Connection parameters for PostgreSQL (included for future expansion)

## Architecture

1. **Client Flow**:
   - Client receives an HTTP request on `/hello`
   - Creates a JSON message with current timestamp and request type
   - Publishes to RabbitMQ with a correlation ID and reply queue
   - Waits for a response on its callback queue
   - Returns the response to the HTTP client

2. **Server Flow**:
   - Consumes messages from the RPC queue
   - Routes requests based on the `request_type` header
   - Processes the request and creates a response with timestamps
   - Publishes the response back to the client's reply queue
   - Uses the correlation ID to ensure the response matches the request

## Message Structure

### Request
```json
{
  "sentAt": "2025-04-12T12:34:56Z"
}
```

### Response
```json
{
  "sentAt": "2025-04-12T12:34:56Z",
  "receivedAt": "2025-04-12T12:34:57Z",
  "respondedAt": "2025-04-12T12:34:57Z"
}
```

## RabbitMQ Management Interface

The RabbitMQ Management UI is available at http://localhost:15672/

- Username: guest
- Password: guest
