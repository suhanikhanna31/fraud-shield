// Command server starts the Fraud Shield HTTP API.
//
// Environment variables:
//
//	PORT          - HTTP listen port (default 8080)
//	DATABASE_URL  - Postgres DSN, e.g. postgres://user:pass@localhost:5432/fraudshield?sslmode=disable
//	               If unset, an in-memory store is used (handy for local dev/demo).
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"fraud-shield/internal/api"
	"fraud-shield/internal/scoring"
	"fraud-shield/internal/store"
)

func main() {
	port := getEnv("PORT", "8080")
	dsn := os.Getenv("DATABASE_URL")

	var s store.Store
	if dsn != "" {
		pg, err := store.NewPostgresStore(dsn)
		if err != nil {
			log.Fatalf("failed to connect to postgres: %v", err)
		}
		s = pg
		log.Println("connected to Postgres store")
	} else {
		s = store.NewMemoryStore()
		log.Println("DATABASE_URL not set; using in-memory store (data will not persist across restarts)")
	}

	engine := scoring.NewEngine(scoring.DefaultConfig())
	server := api.NewServer(engine, s)

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      server.Routes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("fraud-shield listening on :%s", port)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
