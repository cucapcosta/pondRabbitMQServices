package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
)

type SensorData struct {
	SensorID   string  `json:"SensorID"`
	Timestamp  string  `json:"Timestamp"`
	Type       string  `json:"Type"`
	Unit       string  `json:"Unit"`
	IsDiscrete bool    `json:"IsDiscrete"`
	Value      float32 `json:"Value"`
}

const prefetchCount = 100

func main() {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://admin:admin@localhost:5432/sensors?sslmode=disable"
	}

	for {
		if err := run(rabbitURL, dbURL); err != nil {
			log.Printf("Consumer error: %v — retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func run(rabbitURL, dbURL string) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sensor_data (
			id         SERIAL PRIMARY KEY,
			sensor_id  TEXT NOT NULL,
			timestamp  TEXT NOT NULL,
			type       TEXT NOT NULL,
			unit       TEXT NOT NULL,
			is_discrete BOOLEAN NOT NULL DEFAULT FALSE,
			value      REAL NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return err
	}
	log.Println("[DB] Conectado ao PostgreSQL")

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		"hello", true, false, false, false,
		amqp.Table{amqp.QueueTypeArg: amqp.QueueTypeQuorum},
	)
	if err != nil {
		return err
	}

	err = ch.Qos(prefetchCount, 0, false)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		"hello", "", false, false, false, false, nil,
	)
	if err != nil {
		return err
	}

	stmt, err := db.Prepare(`
		INSERT INTO sensor_data (sensor_id, timestamp, type, unit, is_discrete, value)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	log.Printf(" [*] Waiting for messages (prefetch=%d)...", prefetchCount)

	for d := range msgs {
		var data SensorData
		if err := json.Unmarshal(d.Body, &data); err != nil {
			log.Printf("JSON inválido, descartando: %v", err)
			d.Ack(false)
			continue
		}

		_, err := stmt.Exec(data.SensorID, data.Timestamp, data.Type, data.Unit, data.IsDiscrete, data.Value)
		if err != nil {
			log.Printf("Erro ao inserir no DB: %v", err)
			d.Nack(false, true)
			continue
		}

		log.Printf("Salvo: sensor=%s type=%s value=%.2f", data.SensorID, data.Type, data.Value)
		d.Ack(false)
	}

	return nil
}
