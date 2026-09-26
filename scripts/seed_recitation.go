// Run once against MongoDB to load the 4 starter reciters and placeholder
// recitation URLs for Surah Yusuf's first two ayahs (matching the placeholder
// ayahs in seed_quran.go). Replace audio_url values with real EveryAyah.com /
// quran.com CDN URLs before this is anything but a wiring test.
//
// Usage:
//   MONGO_URI="mongodb://tadbor:tadbor_dev_password@localhost:27017" go run scripts/seed_recitation.go
package main

import (
	"context"
	"log"
	"os"
	"time"

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
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("set MONGO_URI first")
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
