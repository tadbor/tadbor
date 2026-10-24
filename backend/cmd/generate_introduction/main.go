package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"
	"tadbor/backend/internal/generation"
	"tadbor/backend/internal/platform"
	"tadbor/backend/internal/retrieval"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("MONGO_URI is not set")
	}

	db, err := platform.NewMongoConnection()
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer platform.CloseMongoConnection(db)

	_ = context.Background()
	_ = db
	_ = retrieval.NewService
	_ = generation.NewService
	log.Println("introduction generation scaffold - requires verified sources")
}
