package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type SensorData struct {
	SensorID   string
	Timestamp  string
	Type       string
	Unit       string
	IsDiscrete bool
	Value      float32
}

const serverPort = 3420

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
}
func main() {
	//Conexão servidor rabbitMQ
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}
	conn, err := amqp.Dial(rabbitURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()
	//Conexão canal rabbitMQ
	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()
	//Setup Fila
	q, err := ch.QueueDeclare(
		"hello", // name
		true,    // durability
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		amqp.Table{
			amqp.QueueTypeArg: amqp.QueueTypeQuorum,
		},
	)
	failOnError(err, "Fail to create Queue")
	//Handle HTTP
	http.HandleFunc("/add", makeAddLog(ch, q.Name))
	fmt.Println("Server listening on port 3420...")
	if err := http.ListenAndServe(":3420", nil); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func makeAddLog(ch *amqp.Channel, queueName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		var data SensorData
		if err := json.Unmarshal(body, &data); err != nil {
			http.Error(w, "Invalid format", http.StatusBadRequest)
			return
		}

		// Publica no RabbitMQ
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = ch.PublishWithContext(ctx,
			"",
			queueName,
			false,
			false,
			amqp.Publishing{ContentType: "application/json", Body: body},
		)
		if err != nil {
			http.Error(w, "Failed to publish to queue", http.StatusInternalServerError)
			return
		}
		fmt.Println("Enviado:", data)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Status   string `json:"status"`
			Message  string `json:"message"`
			SensorID string `json:"sensor_id"`
		}{
			Status:   "success",
			Message:  "Data received and queued",
			SensorID: data.SensorID,
		})
	}
}
