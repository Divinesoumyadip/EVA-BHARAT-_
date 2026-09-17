// Command server is the entrypoint for the Media Sequencer backend.
// It wires configuration, database, and the HTTP layer together and
// starts listening.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/evabharat/media-sequencer/internal/database"
	"github.com/evabharat/media-sequencer/internal/handlers"
	"github.com/evabharat/media-sequencer/internal/repository"
	"github.com/evabharat/media-sequencer/internal/services"
)

func main() {
	port := getEnv("PORT", "8080")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}
	// Default to the Vite dev server's origin rather than "*": CORS should
	// be opened up deliberately for a specific deployed frontend, not
	// wide open by default just because the operator forgot to set it.
	allowedOrigin := getEnv("ALLOWED_ORIGIN", "http://localhost:5173")

	db, err := database.Connect(databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()
	log.Println("database connected")

	if err := database.Migrate(db); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	repo := repository.New(db)
	svc := services.New(repo, time.Now)
	h := handlers.New(svc)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           h.Routes(allowedOrigin),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("server listening on :%s (CORS origin: %s)", port, allowedOrigin)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
