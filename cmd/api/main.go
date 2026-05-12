package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/api"
	braincrypto "github.com/Gentleman-Programming/brain-context/internal/crypto"
	"github.com/Gentleman-Programming/brain-context/internal/store"
)

func main() {
	// 1. Connect to database
	connCtx, connCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer connCancel()

	st, err := store.New(connCtx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer st.Close()

	// 2. Run migrations automatically on every startup
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer migrateCancel()

	if err := store.Migrate(migrateCtx, st.Pool()); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	// 3. Start HTTP server
	server := api.NewServer(st)
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = ":8080"
	}

	// Log encryption status
	if braincrypto.IsConfigured() {
		log.Printf("encryption: BRAIN_ENCRYPTION_KEY configured ✓")
	} else {
		log.Printf("encryption: BRAIN_ENCRYPTION_KEY NOT set — embed key sharing disabled")
	}

	log.Printf("brain-context API listening on %s", address)
	if err := server.Start(address); err != nil {
		log.Fatalf("start server: %v", err)
	}
}
