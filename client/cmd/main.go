package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Request represents the structure of our RPC request
type Request struct {
	SentAt string `json:"sentAt"`
}

// Response represents the structure of our RPC response
type Response struct {
	SentAt      string `json:"sentAt"`      // When the original request was sent
	ReceivedAt  string `json:"receivedAt"`  // When the server received the request
	RespondedAt string `json:"respondedAt"` // When the server sent the response
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

func helloHandler(w http.ResponseWriter, r *http.Request, rpcClient *RPCClient) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Prepare the request with type "hello"
	currentTime := time.Now().Format(time.RFC3339)
	request := Request{
		SentAt: currentTime,
	}

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		log.Printf("Failed to marshal request to JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Make the RPC call, specifying the request type
	response, err := rpcClient.Call(ctx, "hello", jsonRequest)
	if err != nil {
		log.Printf("RPC call failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Parse JSON response
	var parsedResponse Response
	err = json.Unmarshal(response, &parsedResponse)
	if err != nil {
		log.Printf("Failed to unmarshal response JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

func randomString(length int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, length)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
