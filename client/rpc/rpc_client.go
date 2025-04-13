package rpc

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RPCClient holds the client state for making RPC calls
type RPCClient struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	callQueue  string
	replyQueue string
	mutex      sync.Mutex
	pending    map[string]chan []byte
	Timeout    time.Duration
}

// NewRPCClient creates and initializes a new RPC client
func NewRPCClient() (*RPCClient, error) {
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

	callQueue := "rpc_queue"
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

	rpcClient := &RPCClient{
		connection: conn,
		channel:    ch,
		callQueue:  callQueue,
		replyQueue: replyQ.Name,
		pending:    make(map[string]chan []byte),
		Timeout:    getTimeout(),
	}

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

	go rpcClient.handleResponses(msgs)

	log.Printf("RPC client initialized with reply queue: %s", replyQ.Name)
	return rpcClient, nil
}

// handleResponses processes responses coming in on the reply queue
func (c *RPCClient) handleResponses(msgs <-chan amqp.Delivery) {
	for d := range msgs {
		corrID := d.CorrelationId

		responseType, exists := d.Headers["response_type"].(string)
		if !exists {
			log.Printf("Received message without request type header")
			responseType = "unknown"
		}

		c.mutex.Lock()
		resChan, ok := c.pending[corrID]
		c.mutex.Unlock()

		if ok {
			resChan <- d.Body

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
	corrID := randomString(32)
	resChan := make(chan []byte)

	c.mutex.Lock()
	c.pending[corrID] = resChan
	c.mutex.Unlock()

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

func getTimeout() time.Duration {
	// Load the request timeout from environment variable
	timeoutRaw := os.Getenv("CLIENT_REQUEST_TIMEOUT")

	// Parse the timeout value from the environment variable to an integer, defaulting to 10 seconds in milliseconds
	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil {
		log.Printf("Invalid timeout value, using default: %v", err)
		timeout = 10 * time.Second
	}

	return timeout
}

func randomString(length int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, length)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
