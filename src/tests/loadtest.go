package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
)

const (
	defaultDuration = 30 * time.Second
	defaultWorkers  = 10
	defaultURL      = "http://back:3420/add"
	defaultDBURL    = "postgres://admin:admin@postgres:5432/sensors?sslmode=disable"
	verifyTimeout   = 60 * time.Second
	progressEvery   = 5 * time.Second
)

type SensorData struct {
	SensorID   string  `json:"SensorID"`
	Timestamp  string  `json:"Timestamp"`
	Type       string  `json:"Type"`
	Unit       string  `json:"Unit"`
	IsDiscrete bool    `json:"IsDiscrete"`
	Value      float32 `json:"Value"`
}

type Stats struct {
	sent       atomic.Int64
	success    atomic.Int64
	errConnect atomic.Int64
	errTimeout atomic.Int64
	errHTTP    atomic.Int64
}

func main() {
	targetURL := envOr("TARGET_URL", defaultURL)
	dbURL := envOr("DATABASE_URL", defaultDBURL)
	duration := envDuration("DURATION", defaultDuration)
	workers := envInt("WORKERS", defaultWorkers)

	log.Printf("=== Load Test ===")
	log.Printf("Target:   %s", targetURL)
	log.Printf("Duration: %s", duration)
	log.Printf("Workers:  %d", workers)

	waitForBackend(targetURL, 30*time.Second)
	cleanDB(dbURL)

	stats := &Stats{}
	produce(targetURL, duration, workers, stats)
	printReport(stats, duration)
	waitAndVerify(dbURL, stats.success.Load())
}

func waitForBackend(url string, timeout time.Duration) {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(strings.TrimSuffix(url, "/add"))
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			log.Println("Backend is ready")
			return
		}
		log.Printf("Waiting for backend... (%v)", err)
		time.Sleep(2 * time.Second)
	}
	log.Println("WARNING: backend may not be ready, proceeding anyway")
}

func produce(targetURL string, duration time.Duration, workers int, stats *Stats) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: workers,
		},
	}

	deadline := time.Now().Add(duration)
	done := make(chan struct{})

	// Progress ticker
	go func() {
		ticker := time.NewTicker(progressEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				log.Printf("[progress] sent=%d success=%d errConn=%d errTimeout=%d errHTTP=%d",
					stats.sent.Load(), stats.success.Load(),
					stats.errConnect.Load(), stats.errTimeout.Load(), stats.errHTTP.Load())
			}
		}
	}()

	// Launch workers
	finished := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			for time.Now().Before(deadline) {
				sendOne(client, targetURL, stats, rng, workerID)
			}
			finished <- struct{}{}
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-finished
	}
	close(done)
}

func sendOne(client *http.Client, url string, stats *Stats, rng *rand.Rand, workerID int) {
	sensorTypes := []string{"temperature", "humidity", "pressure", "luminosity"}
	units := []string{"°C", "%", "hPa", "lux"}
	idx := rng.Intn(len(sensorTypes))

	data := SensorData{
		SensorID:   fmt.Sprintf("sensor-%d-%d", workerID, stats.sent.Load()),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Type:       sensorTypes[idx],
		Unit:       units[idx],
		IsDiscrete: false,
		Value:      float32(rng.Intn(1000)) / 10.0,
	}

	body, _ := json.Marshal(data)
	stats.sent.Add(1)

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "dial tcp"):
			stats.errConnect.Add(1)
		case strings.Contains(errMsg, "timeout") ||
			strings.Contains(errMsg, "deadline exceeded"):
			stats.errTimeout.Add(1)
		default:
			stats.errHTTP.Add(1)
		}
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		stats.success.Add(1)
	} else {
		stats.errHTTP.Add(1)
	}
}

func printReport(stats *Stats, duration time.Duration) {
	sent := stats.sent.Load()
	success := stats.success.Load()
	errConn := stats.errConnect.Load()
	errTimeout := stats.errTimeout.Load()
	errHTTP := stats.errHTTP.Load()
	avgRPS := float64(sent) / duration.Seconds()

	fmt.Println()
	fmt.Println("========== LOAD TEST REPORT ==========")
	fmt.Printf("Duration:       %s\n", duration)
	fmt.Printf("Total sent:     %d\n", sent)
	fmt.Printf("Success (200):  %d\n", success)
	fmt.Printf("Err connect:    %d\n", errConn)
	fmt.Printf("Err timeout:    %d\n", errTimeout)
	fmt.Printf("Err HTTP:       %d\n", errHTTP)
	fmt.Printf("Avg req/s:      %.1f\n", avgRPS)
	fmt.Println("=======================================")
	fmt.Println()
}

func cleanDB(dbURL string) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("WARNING: cannot connect to DB for cleanup: %v", err)
		return
	}
	defer db.Close()

	var count int64
	db.QueryRow("SELECT COUNT(*) FROM sensor_data").Scan(&count)
	if count > 0 {
		log.Printf("Cleaning DB: deleting %d existing rows...", count)
	}

	_, err = db.Exec("TRUNCATE TABLE sensor_data RESTART IDENTITY")
	if err != nil {
		log.Printf("WARNING: TRUNCATE failed: %v", err)
		return
	}
	log.Println("DB cleaned")
}

func waitAndVerify(dbURL string, expectedCount int64) {
	log.Printf("=== Verification Phase ===")
	log.Printf("Expected rows: %d (based on HTTP 200 responses)", expectedCount)

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("FAIL: cannot connect to database: %v", err)
		return
	}
	defer db.Close()

	deadline := time.Now().Add(verifyTimeout)
	var count int64

	for time.Now().Before(deadline) {
		err = db.QueryRow("SELECT COUNT(*) FROM sensor_data").Scan(&count)
		if err != nil {
			log.Printf("Query error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Printf("DB rows: %d / %d expected", count, expectedCount)

		if count >= expectedCount {
			fmt.Println()
			fmt.Println("PASS: all messages arrived in the database")
			fmt.Printf("  Expected: %d | Found: %d\n", expectedCount, count)
			fmt.Println()
			return
		}

		time.Sleep(2 * time.Second)
	}

	fmt.Println()
	fmt.Println("FAIL: not all messages arrived within timeout")
	fmt.Printf("  Expected: %d | Found: %d | Missing: %d\n", expectedCount, count, expectedCount-count)
	fmt.Println()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("Invalid duration %q for %s, using default %s", v, key, fallback)
		return fallback
	}
	return d
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("Invalid int %q for %s, using default %d", v, key, fallback)
		return fallback
	}
	return n
}
