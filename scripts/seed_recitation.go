// Loads the 4 reciters. Audio URLs are NOT seeded here — that is
// backend/cmd/ingest_recitations (issue #14), which writes all 444 per-ayah
// recitations from EveryAyah.com after checking every URL.
//
// This script used to write 8 placeholder recitations on example.com for ayahs
// 1 and 2. They are gone, deliberately: `make seed` is the command a new clone
// runs first, so a placeholder written here came straight back the moment anyone
// re-seeded, overwriting real audio with example.com URLs that do not play. The
// reciters are kept because they are reference data the ingestion command needs
// to exist; the audio is not reference data and is verified per run.
//
// The Quran text itself is NOT seeded here: that is backend/cmd/seed_quran, which
// loads the verified Uthmani corpus for all 111 ayahs.
//
// Reads .env like every backend command, though godotenv only looks at ./.env and
// this repo's .env is at the root — `make seed` sources it before getting here.
//
// Usage:
//
//	cd backend && go run ../scripts/seed_recitation.go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type reciterSeed struct {
	ID   string `bson:"_id"`
	Name string `bson:"name"`
}

type recitationSeed struct {
	ReciterID  string `bson:"reciter_id"`
	SurahID    int    `bson:"surah_id"`
	AyahNumber int    `bson:"ayah_number"`
	AudioURL   string `bson:"audio_url"`
}

func main() {
	// .env is optional — `make seed` and docker-compose pass values directly.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("godotenv: %v", err)
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("MONGO_URI is not set (put it in .env, or export it)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("tadbor")

	reciters := []reciterSeed{
		{ID: "alafasy", Name: "Mishary Rashid Alafasy"},
		{ID: "abdulbasit", Name: "Abdul Basit Abdus Samad"},
		{ID: "ghamdi", Name: "Saad Al-Ghamdi"},
		{ID: "muaiqly", Name: "Maher Al-Muaiqly"},
	}
	var ops []mongo.WriteModel
	for _, r := range reciters {
		ops = append(ops, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "_id", Value: r.ID}}).
			SetUpdate(bson.D{{Key: "$set", Value: r}}).
			SetUpsert(true))
	}
	if _, err := db.Collection("reciters").BulkWrite(ctx, ops); err != nil {
		log.Fatal(err)
	}

	log.Printf("upserted %d reciters; run backend/cmd/ingest_recitations -live to load audio URLs\n", len(ops))
}
