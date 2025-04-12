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

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func processMessage(d amqp.Delivery, ch *amqp.Channel) {
	// Create a context with timeout for publishing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Record the received time
	receivedTime := time.Now().Format(time.RFC3339)
	log.Printf("Received a message with correlation ID: %s, reply to: %s", d.CorrelationId, d.ReplyTo)

	// Parse the JSON request
	var request Request
	err := json.Unmarshal(d.Body, &request)
	if err != nil {
		log.Printf("Error parsing request JSON: %v", err)
		d.Nack(false, false) // Reject the message without requeue
		return
	}

	log.Printf("Request sent at: %s, received at: %s", request.SentAt, receivedTime)

	// Small delay to simulate processing time (helps with debugging)
	time.Sleep(100 * time.Millisecond)

	// Prepare response with all timestamps
	respondedAt := time.Now().Format(time.RFC3339)
	response := Response{
		SentAt:      request.SentAt,
		ReceivedAt:  receivedTime,
		RespondedAt: respondedAt,
	}

	// Convert response to JSON
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error creating JSON response: %v", err)
		d.Nack(false, false) // Reject the message without requeue
		return
	}

	if d.ReplyTo == "" {
		log.Printf("Missing reply queue in request. Cannot respond.")
		d.Nack(false, false)
		return
	}

	// Publish the response
	err = ch.PublishWithContext(ctx,
		"",        // exchange
		d.ReplyTo, // routing key - the client's unique reply queue
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: d.CorrelationId,
			Body:          jsonResponse,
		})

	if err != nil {
		log.Printf("Error publishing response: %v", err)
		d.Nack(false, false) // Reject the message without requeue
		return
	}

	// Acknowledge the message
	d.Ack(false)
	log.Printf("Sent response to queue %s with correlation ID: %s", d.ReplyTo, d.CorrelationId)
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
		"rpc_queue", // name
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
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

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
			go processMessage(d, ch)
		}

		done <- true
	}()

	// Wait for shutdown signal
	<-stopChan
	log.Println("Shutting down server gracefully...")

	// Close channel and wait for worker to finish
	if err := ch.Cancel("", false); err != nil {
		log.Printf("Error canceling consumer: %v", err)
	}

	// Wait for workers to finish processing
	<-done
	log.Println("Server shutdown complete")
}
