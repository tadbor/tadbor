package platform

import (
	"context"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// NewMongoConnection opens a connection using MONGO_URI / MONGO_DB_NAME env vars
// (set in docker-compose.yml). Returns the *mongo.Database handle every
// internal service should be constructed with.
func NewMongoConnection() (*mongo.Database, error) {
	uri := os.Getenv("MONGO_URI")
	dbName := os.Getenv("MONGO_DB_NAME")
	if dbName == "" {
		dbName = "tadbor"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return client.Database(dbName), nil
}

func CloseMongoConnection(db *mongo.Database) {
	_ = db.Client().Disconnect(context.Background())
}
