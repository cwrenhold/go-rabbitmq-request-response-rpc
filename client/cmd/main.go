package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Request represents the structure of our RPC request
type Request struct {
	SentAt string `json:"sentAt"`
}

// Response represents the structure of our RPC response
type Response struct {
	SentAt      string `json:"sentAt"`
	ReceivedAt  string `json:"receivedAt"`
	RespondedAt string `json:"respondedAt"`
}

func main() {
	port := os.Getenv("CLIENT_PORT")
	if port == "" {
		port = "8080" // Default to 8080 if CLIENT_PORT is not set
	}

	// RabbitMQ connection setup
	log.Printf("Connecting to RabbitMQ on %s:%s as %s", os.Getenv("RABBITMQ_HOSTNAME"), os.Getenv("RABBITMQ_PORT"), os.Getenv("RABBITMQ_USER"))
	rabbitMQURL := fmt.Sprintf("amqp://%s:%s@%s:%s/", os.Getenv("RABBITMQ_USER"), os.Getenv("RABBITMQ_PASSWORD"), os.Getenv("RABBITMQ_HOSTNAME"), os.Getenv("RABBITMQ_PORT"))
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		helloHandler(w, r, ch)
	})

	log.Printf("Starting HTTP server on port %s", port)
	http.ListenAndServe(":"+port, nil)
}

func helloHandler(w http.ResponseWriter, r *http.Request, ch *amqp.Channel) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Create a unique reply queue for this request
	replyQ, err := ch.QueueDeclare(
		"",    // name (empty = let server generate a unique name)
		false, // durable
		true,  // delete when unused (auto-delete)
		true,  // exclusive (only used by this connection)
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		log.Printf("Failed to declare reply queue: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Generate a correlation ID for this request
	corrID := randomString(32)
	currentTime := time.Now().Format(time.RFC3339)

	// Create request message as JSON
	request := Request{
		SentAt: currentTime,
	}

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		log.Printf("Failed to marshal request to JSON: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Create a consumer for the reply queue BEFORE publishing the request
	msgs, err := ch.Consume(
		replyQ.Name, // queue
		"",          // consumer tag (empty = let server generate)
		true,        // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		log.Printf("Failed to register a consumer: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Publish message to RabbitMQ
	log.Printf("Publishing request (%s) to RabbitMQ: %s", corrID, string(jsonRequest))
	err = ch.PublishWithContext(
		ctx,
		"",          // exchange
		"rpc_queue", // routing key (queue name)
		false,       // mandatory
		false,       // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: corrID,
			ReplyTo:       replyQ.Name, // Use our unique reply queue
			Body:          jsonRequest,
		},
	)
	if err != nil {
		log.Printf("Failed to publish a message: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Create a channel to signal when we're done processing messages
	done := make(chan struct{})

	// Create a response channel to get our result
	responseChan := make(chan []byte)

	// Start a goroutine to consume from our reply queue
	go func() {
		for d := range msgs {
			if d.CorrelationId == corrID {
				log.Printf("Received response for correlation ID %s", corrID)
				responseChan <- d.Body
				done <- struct{}{} // Signal that we're done
				return
			} else {
				log.Printf("Received message with wrong correlation ID: expected %s, got %s", corrID, d.CorrelationId)
			}
		}
	}()

	// Wait for either response or timeout
	select {
	case responseBody := <-responseChan:
		// Parse JSON response
		var response Response
		err := json.Unmarshal(responseBody, &response)
		if err != nil {
			log.Printf("Failed to unmarshal response JSON: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Set content type to JSON and return the full JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(responseBody)
		log.Printf("Successfully processed RPC request with correlation ID %s", corrID)

		// Ensure we close our goroutine
		<-done
		return

	case <-ctx.Done():
		log.Printf("Request timed out for correlation ID %s", corrID)
		w.WriteHeader(http.StatusGatewayTimeout)
		w.Write([]byte(`{"error": "Request timed out"}`))
		return
	}
}

func randomString(length int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, length)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
