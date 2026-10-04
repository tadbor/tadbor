// Run once against MongoDB to load the 4 starter reciters and placeholder
// recitation URLs for Surah Yusuf's first two ayahs. Replace audio_url values with
// real EveryAyah.com / quran.com CDN URLs before this is anything but a wiring test
// (issue #14).
//
// The Quran text itself is NOT seeded here: that is backend/cmd/seed_quran, which
// loads the verified Uthmani corpus for all 111 ayahs. This script only covers the
// two ayahs it has placeholder audio for, which is why the surah page shows audio
// controls on the first two verses and nowhere else.
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

	// PLACEHOLDER URLs — replace with real per-ayah audio (e.g. EveryAyah.com
	// or quran.com's audio CDN) before treating this as real data.
	var recitationOps []mongo.WriteModel
	for _, r := range reciters {
		for ayah := 1; ayah <= 2; ayah++ {
			rec := recitationSeed{
				ReciterID:  r.ID,
				SurahID:    12,
				AyahNumber: ayah,
				AudioURL:   "https://example.com/placeholder/" + r.ID + "/012" + itoa(ayah) + ".mp3",
			}
			recitationOps = append(recitationOps, mongo.NewUpdateOneModel().
				SetFilter(bson.D{
					{Key: "reciter_id", Value: r.ID},
					{Key: "surah_id", Value: 12},
					{Key: "ayah_number", Value: ayah},
				}).
				SetUpdate(bson.D{{Key: "$set", Value: rec}}).
				SetUpsert(true))
		}
	}
	if _, err := db.Collection("recitations").BulkWrite(ctx, recitationOps); err != nil {
		log.Fatal(err)
	}

	log.Printf("upserted %d reciters and %d placeholder recitations\n", len(ops), len(recitationOps))
}

func itoa(n int) string {
	if n < 10 {
		return "00" + string(rune('0'+n))
	}
	return "0" + string(rune('0'+n))
}
