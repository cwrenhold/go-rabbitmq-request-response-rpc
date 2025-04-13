package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func HandleRPCRequests(router *RequestRouter) {
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

	// Create done channel for worker goroutines
	done := make(chan bool)

	// Start worker goroutine
	go func() {
		log.Printf("Server is waiting for RPC requests on queue: %s", q.Name)

		for d := range msgs {
			if d.ReplyTo == "" {
				log.Println("Received message with no reply-to queue")
				d.Nack(false, false)
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			receivedTime := time.Now().Format(time.RFC3339)

			requestType := ""
			if d.Headers != nil {
				if typeVal, ok := d.Headers["request_type"]; ok {
					if typeStr, ok := typeVal.(string); ok {
						requestType = typeStr
					}
				}
			}

			if requestType == "" {
				log.Println("Received message with no request_type header")
				d.Nack(false, false)
				cancel()
				continue
			}

			log.Printf("Received request of type '%s' with correlation ID: %s", requestType, d.CorrelationId)

			response, err := router.RouteRequest(ctx, requestType, d.Body, receivedTime)
			if err != nil {
				log.Printf("Error handling request: %v", err)
				d.Nack(false, false)
				cancel()
				continue
			}

			jsonResponse, err := json.Marshal(response)
			if err != nil {
				log.Printf("Error creating JSON response: %v", err)
				d.Nack(false, false)
				cancel()
				continue
			}

			err = ch.PublishWithContext(ctx,
				"",        // exchange
				d.ReplyTo, // routing key - the client's callback queue
				false,     // mandatory
				false,     // immediate
				amqp.Publishing{
					ContentType:   "application/json",
					CorrelationId: d.CorrelationId,
					Body:          jsonResponse,
					Headers: amqp.Table{
						"response_type": requestType,
					},
				},
			)

			cancel()

			if err != nil {
				log.Printf("Error publishing response: %v", err)
				d.Nack(false, false)
				continue
			}

			d.Ack(false)
			log.Printf("Sent '%s' response to queue %s with correlation ID: %s",
				requestType, d.ReplyTo, d.CorrelationId)
		}

		done <- true
	}()

	<-done
	log.Println("Server shutdown complete")
}
