package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

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
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
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
	err = ch.PublishWithContext()
	//Handle HTTP
	http.HandleFunc("/add", addLog)
	fmt.Println("Server listening on port 3420...")
	if err := http.ListenAndServe(":3420", nil); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func addLog(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Errorf("Error during reading body: %v", err)
		}
		var data SensorData
		if err := json.Unmarshal(body, &data); err != nil {
			http.Error(w, "Invalid format", http.StatusBadRequest)
			return
		}
		fmt.Println(data)
		w.Header().Set("Content-Type", "application/json")
		responseData := struct {
			Status   string `json:"status"`
			Message  string `json:"message"`
			SensorID string `json:"sensor_id"`
		}{
			Status:   "success",
			Message:  "Data received successfully",
			SensorID: data.SensorID,
		}

		// Encode and send the JSON response
		if err := json.NewEncoder(w).Encode(responseData); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
