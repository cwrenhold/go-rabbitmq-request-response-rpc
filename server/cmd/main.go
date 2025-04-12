package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Base request and response types
type RequestMetadata struct {
	SentAt string `json:"sentAt"`
}

// AddRequest extends the base Request for the "add" endpoint
type AddRequest struct {
	RequestMetadata
	Values []int `json:"values"`
}

// ResponseMetadata represents the structure of our RPC response
type ResponseMetadata struct {
	SentAt      string `json:"sentAt"`
	ReceivedAt  string `json:"receivedAt"`
	RespondedAt string `json:"respondedAt"`
}

// AddResponse extends the base Response for the "add" endpoint
type AddResponse struct {
	ResponseMetadata
	Sum int `json:"sum"`
}

// RequestHandler is a function type for request handlers that receives raw message body
type RequestHandler func(context.Context, []byte, string, string) (interface{}, error)

// RequestRouter routes requests to appropriate handlers
type RequestRouter struct {
	handlers map[string]RequestHandler
}

// NewRequestRouter creates a new router with default handlers
func NewRequestRouter() *RequestRouter {
	router := &RequestRouter{
		handlers: make(map[string]RequestHandler),
	}

	// Register the default hello handler
	router.RegisterHandler("hello", handleHelloRequest)

	// Register the add handler
	router.RegisterHandler("add", handleAddRequest)

	return router
}

// RegisterHandler adds a new handler for a specific request type
func (r *RequestRouter) RegisterHandler(requestType string, handler RequestHandler) {
	r.handlers[requestType] = handler
}

// RouteRequest routes a request to the appropriate handler
func (r *RequestRouter) RouteRequest(ctx context.Context, requestType string, body []byte, receivedTime string) (interface{}, error) {
	handler, exists := r.handlers[requestType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for request type: %s", requestType)
	}

	return handler(ctx, body, requestType, receivedTime)
}

func buildMetadata(sentAt, receivedAt string) ResponseMetadata {
	return buildMetadataWithRespondedAt(sentAt, receivedAt, time.Now().Format(time.RFC3339))
}

func buildMetadataWithRespondedAt(sentAt, receivedAt, respondedAt string) ResponseMetadata {
	return ResponseMetadata{
		SentAt:      sentAt,
		ReceivedAt:  receivedAt,
		RespondedAt: respondedAt,
	}
}

// handleHelloRequest is the handler for "hello" type requests
func handleHelloRequest(ctx context.Context, body []byte, requestType string, receivedTime string) (interface{}, error) {
	// Parse the request body
	var req RequestMetadata
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hello request: %v", err)
	}

	// The hello handler simply echoes back with timestamps
	return buildMetadata(req.SentAt, receivedTime), nil
}

// handleAddRequest is the handler for "add" type requests
func handleAddRequest(ctx context.Context, body []byte, requestType string, receivedTime string) (interface{}, error) {
	// Parse the request body directly to an AddRequest
	var addReq AddRequest
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
	return AddResponse{
		ResponseMetadata: buildMetadata(addReq.SentAt, receivedTime),
		Sum:              sum,
	}, nil
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func main() {
	// RabbitMQ connection setup using environment variables
	log.Printf("Server connecting to RabbitMQ on %s:%s as %s", os.Getenv("RABBITMQ_HOSTNAME"), os.Getenv("RABBITMQ_PORT"), os.Getenv("RABBITMQ_USER"))
	rabbitMQURL := fmt.Sprintf("amqp://%s:%s@%s:%s/",
		os.Getenv("RABBITMQ_USER"),
		os.Getenv("RABBITMQ_PASSWORD"),
		os.Getenv("RABBITMQ_HOSTNAME"),
		os.Getenv("RABBITMQ_PORT"))

	conn, err := amqp.Dial(rabbitMQURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"rpc_queue", // name - the queue clients will send requests to
		false,       // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	failOnError(err, "Failed to declare a queue")

	// Set QoS to allow multiple unacknowledged messages - helps with throughput
	err = ch.Qos(
		5,     // prefetch count - process 5 messages at a time
		0,     // prefetch size
		false, // global
	)
	failOnError(err, "Failed to set QoS")

	// Start consuming from the queue
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack - important to set this to false for RPC
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	// Create a request router
	router := NewRequestRouter()

	// Create a channel to handle graceful shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	// Create done channel for worker goroutines
	done := make(chan bool)

	// Start worker goroutine
	go func() {
		log.Printf("Server is waiting for RPC requests on queue: %s", q.Name)

		// Process incoming messages
		for d := range msgs {
			// Ensure we have a reply-to address
			if d.ReplyTo == "" {
				log.Println("Received message with no reply-to queue")
				d.Nack(false, false)
				continue
			}

			// Create a context with timeout for processing
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			// Record the received time
			receivedTime := time.Now().Format(time.RFC3339)

			// Extract request type from headers - this is now the only source of truth
			requestType := ""
			if d.Headers != nil {
				if typeVal, ok := d.Headers["request_type"]; ok {
					if typeStr, ok := typeVal.(string); ok {
						requestType = typeStr
					}
				}
			}

			// Check if request type is available
			if requestType == "" {
				log.Println("Received message with no request_type header")
				d.Nack(false, false) // Reject the message without requeue
				cancel()
				continue
			}

			log.Printf("Received request of type '%s' with correlation ID: %s", requestType, d.CorrelationId)

			// Route the request to the appropriate handler
			// Pass the raw message body to let the handler do the unmarshalling
			response, err := router.RouteRequest(ctx, requestType, d.Body, receivedTime)
			if err != nil {
				log.Printf("Error handling request: %v", err)
				d.Nack(false, false) // Reject the message without requeue
				cancel()
				continue
			}

			// Convert response to JSON
			jsonResponse, err := json.Marshal(response)
			if err != nil {
				log.Printf("Error creating JSON response: %v", err)
				d.Nack(false, false) // Reject the message without requeue
				cancel()
				continue
			}

			// Publish the response to the client's callback queue
			err = ch.PublishWithContext(ctx,
				"",        // exchange
				d.ReplyTo, // routing key - the client's callback queue
				false,     // mandatory
				false,     // immediate
				amqp.Publishing{
					ContentType:   "application/json",
					CorrelationId: d.CorrelationId, // Use the same correlation ID from the request
					Body:          jsonResponse,
					Headers: amqp.Table{
						"response_type": requestType,
					},
				})

			cancel() // Release the context resources

			if err != nil {
				log.Printf("Error publishing response: %v", err)
				d.Nack(false, false) // Reject the message without requeue
				continue
			}

			// Acknowledge the message - we've processed it successfully
			d.Ack(false)
			log.Printf("Sent '%s' response to queue %s with correlation ID: %s",
				requestType, d.ReplyTo, d.CorrelationId)
		}

		done <- true
	}()

	// Wait for shutdown signal
	<-stopChan
	log.Println("Shutting down server gracefully...")

	// Cancel consumer
	if err := ch.Cancel("", false); err != nil {
		log.Printf("Error canceling consumer: %v", err)
	}

	// Wait for worker to finish
	<-done
	log.Println("Server shutdown complete")
}
