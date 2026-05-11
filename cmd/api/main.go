package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Gentleman-Programming/brain-context/internal/api"
	"github.com/Gentleman-Programming/brain-context/internal/store"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := store.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	server := api.NewServer(st)
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = ":8080"
	}

	if err := server.Start(address); err != nil {
		log.Fatal(err)
	}
}
