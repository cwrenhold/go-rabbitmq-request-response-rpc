package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RequestMetadata represents the structure of our RPC request
type RequestMetadata struct {
	SentAt string `json:"sentAt"`
}

// AddRequest extends the base Request for the "add" endpoint
type AddRequest struct {
	RequestMetadata
	Values []int `json:"values"` // The values to be added together
}

// ResponseMetadata represents the structure of our RPC response
type ResponseMetadata struct {
	SentAt      string `json:"sentAt"`      // When the original request was sent
	ReceivedAt  string `json:"receivedAt"`  // When the server received the request
	RespondedAt string `json:"respondedAt"` // When the server sent the response
}

// AddResponse extends the base Response for the "add" endpoint
type AddResponse struct {
	ResponseMetadata
	Sum int `json:"sum"` // The sum of the values
}

// RPCClient holds the client state for making RPC calls
type RPCClient struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	callQueue  string
	replyQueue string
	mutex      sync.Mutex
	pending    map[string]chan []byte
}

func main() {
	port := os.Getenv("CLIENT_PORT")
	if port == "" {
		port = "8080" // Default to 8080 if CLIENT_PORT is not set
	}

	// Initialize RPC client
	rpcClient, err := NewRPCClient()
	if err != nil {
		log.Fatalf("Failed to initialize RPC client: %v", err)
	}
	defer rpcClient.Close()

	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		helloHandler(w, r, rpcClient)
	})

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		addHandler(w, r, rpcClient)
	})

	log.Printf("Starting HTTP server on port %s", port)
	http.ListenAndServe(":"+port, nil)
}

// NewRPCClient creates and initializes a new RPC client
func NewRPCClient() (*RPCClient, error) {
	// RabbitMQ connection setup
	log.Printf("Connecting to RabbitMQ on %s:%s as %s", os.Getenv("RABBITMQ_HOSTNAME"), os.Getenv("RABBITMQ_PORT"), os.Getenv("RABBITMQ_USER"))
	rabbitMQURL := fmt.Sprintf("amqp://%s:%s@%s:%s/",
		os.Getenv("RABBITMQ_USER"),
		os.Getenv("RABBITMQ_PASSWORD"),
		os.Getenv("RABBITMQ_HOSTNAME"),
		os.Getenv("RABBITMQ_PORT"))

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open a channel: %v", err)
	}

	// Declare the queue we'll send RPC requests to
	callQueue := "rpc_queue"

	// Declare a reply queue for all responses (exclusive to this connection)
	replyQ, err := ch.QueueDeclare(
		"",    // name (let server generate a unique name)
		false, // durable
		true,  // delete when unused
		true,  // exclusive (used only by this connection)
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare reply queue: %v", err)
	}

	// Initialize the client
	rpcClient := &RPCClient{
		connection: conn,
		channel:    ch,
		callQueue:  callQueue,
		replyQueue: replyQ.Name,
		pending:    make(map[string]chan []byte),
	}

	// Start consuming from the reply queue
	msgs, err := ch.Consume(
		replyQ.Name, // queue
		"",          // consumer
		true,        // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		rpcClient.Close()
		return nil, fmt.Errorf("failed to consume from reply queue: %v", err)
	}

	// Handle incoming messages in a separate goroutine
	go rpcClient.handleResponses(msgs)

	log.Printf("RPC client initialized with reply queue: %s", replyQ.Name)
	return rpcClient, nil
}

// handleResponses processes responses coming in on the reply queue
func (c *RPCClient) handleResponses(msgs <-chan amqp.Delivery) {
	for d := range msgs {
		corrID := d.CorrelationId

		// Find the type of request from the headers
		responseType, exists := d.Headers["response_type"].(string)
		if !exists {
			log.Printf("Received message without request type header")
			responseType = "unknown"
		}

		// Find the channel for this correlation ID
		c.mutex.Lock()
		resChan, ok := c.pending[corrID]
		c.mutex.Unlock()

		if ok {
			// Pass the response to the waiting goroutine
			resChan <- d.Body

			// Clean up once we've delivered the response
			c.mutex.Lock()
			delete(c.pending, corrID)
			c.mutex.Unlock()

			log.Printf("Received '%s' response for correlation ID: %s", responseType, corrID)
		} else {
			log.Printf("Received '%s' message with unknown correlation ID: %s", responseType, corrID)
		}
	}
}

// Call makes an RPC request and waits for a response
func (c *RPCClient) Call(ctx context.Context, requestType string, body []byte) ([]byte, error) {
	// Generate a correlation ID
	corrID := randomString(32)

	// Create a channel to receive the response
	resChan := make(chan []byte)

	// Register this correlation ID
	c.mutex.Lock()
	c.pending[corrID] = resChan
	c.mutex.Unlock()

	// Publish the message with our correlation ID and reply queue
	err := c.channel.PublishWithContext(
		ctx,
		"",          // exchange
		c.callQueue, // routing key
		false,       // mandatory
		false,       // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: corrID,
			ReplyTo:       c.replyQueue,
			Body:          body,
			// Add a header to indicate the request type
			Headers: amqp.Table{
				"request_type": requestType,
			},
		},
	)
	if err != nil {
		c.mutex.Lock()
		delete(c.pending, corrID)
		c.mutex.Unlock()
		return nil, fmt.Errorf("failed to publish message: %v", err)
	}

	log.Printf("Published '%s' request with correlation ID: %s", requestType, corrID)

	// Wait for the response or timeout
	select {
	case res := <-resChan:
		return res, nil
	case <-ctx.Done():
		c.mutex.Lock()
		delete(c.pending, corrID)
		c.mutex.Unlock()
		return nil, fmt.Errorf("request timed out")
	}
}

// Close closes the RPC client
func (c *RPCClient) Close() {
	if c.channel != nil {
		c.channel.Close()
	}
	if c.connection != nil {
		c.connection.Close()
	}
}

// handleRPCRequest is a generic handler for RPC requests
// requestCreator creates the request object
// requestType is the type of request being made
// responseProcessor performs any post-processing of the response
func handleRPCRequest(
	w http.ResponseWriter,
	r *http.Request,
	rpcClient *RPCClient,
	requestType string,
	requestCreator func() (interface{}, error),
	responseProcessor func([]byte) ([]byte, error),
) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Create the request
	req, err := requestCreator()
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Marshal request to JSON
	jsonRequest, err := json.Marshal(req)
	if err != nil {
		log.Printf("Failed to marshal request to JSON: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Make the RPC call
	response, err := rpcClient.Call(ctx, requestType, jsonRequest)
	if err != nil {
		log.Printf("RPC call failed: %v", err)
		http.Error(w, "RPC call failed", http.StatusInternalServerError)
		return
	}

	// Parse the response
	processedResponse, err := responseProcessor(response)
	if err != nil {
		log.Printf("Failed to process response: %v", err)
		http.Error(w, "Failed to parse response", http.StatusInternalServerError)
		return
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(processedResponse)
}

func helloHandler(w http.ResponseWriter, r *http.Request, rpcClient *RPCClient) {
	handleRPCRequest(w, r, rpcClient, "hello",
		// Request creator function
		func() (interface{}, error) {
			currentTime := time.Now().Format(time.RFC3339)
			return RequestMetadata{SentAt: currentTime}, nil
		},
		// Response parser function
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}

// addHandler processes requests to the /add endpoint
func addHandler(w http.ResponseWriter, r *http.Request, rpcClient *RPCClient) {
	handleRPCRequest(w, r, rpcClient, "add",
		// Request creator function
		func() (interface{}, error) {
			// Parse query parameters
			queryValues := r.URL.Query()["val"]
			if len(queryValues) == 0 {
				return nil, fmt.Errorf("missing 'val' query parameter")
			}

			// Convert query parameters to integers
			values := make([]int, 0, len(queryValues))
			for _, valStr := range queryValues {
				val, err := strconv.Atoi(valStr)
				if err != nil {
					return nil, fmt.Errorf("invalid value '%s': must be an integer", valStr)
				}
				values = append(values, val)
			}

			// Prepare the add request
			currentTime := time.Now().Format(time.RFC3339)
			return AddRequest{
				RequestMetadata: RequestMetadata{
					SentAt: currentTime,
				},
				Values: values,
			}, nil
		},
		// Response parser function
		func(response []byte) ([]byte, error) {
			return response, nil
		})
}

func randomString(length int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, length)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
