// Run this once against your MongoDB instance to load Surah Yusuf.
// Usage (from repo root, with the mongodb container running):
//   MONGO_URI="mongodb://tadbor:tadbor_dev_password@localhost:27017" go run scripts/seed_quran.go
//
// Replace the placeholder ayah text below with your chosen canonical source
// (Part 1 §5) before treating this data as real — this file ships with only
// a couple of placeholder rows so the pipeline is runnable end-to-end.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ayahSeed struct {
	SurahID       int    `bson:"surah_id"`
	AyahNumber    int    `bson:"ayah_number"`
	TextUthmani   string `bson:"text_uthmani"`
	TextSimple    string `bson:"text_simple"`
	CorpusVersion string `bson:"corpus_version"`
	ContentHash   string `bson:"content_hash"`
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
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

	col := client.Database("tadbor").Collection("ayahs")

	// PLACEHOLDER DATA — replace with your verified source text (Phase 0/1 of
	// docs/build-deploy-guide.md) before this is anything but a wiring test.
	placeholder := []ayahSeed{
		{SurahID: 12, AyahNumber: 1, TextUthmani: "الر ۚ تِلْكَ آيَاتُ الْكِتَابِ الْمُبِينِ", TextSimple: "الر تلك آيات الكتاب المبين", CorpusVersion: "v0-placeholder"},
		{SurahID: 12, AyahNumber: 2, TextUthmani: "إِنَّا أَنزَلْنَاهُ قُرْآنًا عَرَبِيًّا لَّعَلَّكُمْ تَعْقِلُونَ", TextSimple: "إنا أنزلناه قرآنا عربيا لعلكم تعقلون", CorpusVersion: "v0-placeholder"},
	}

	var ops []mongo.WriteModel
	for _, a := range placeholder {
		a.ContentHash = hash(a.TextUthmani)
		ops = append(ops, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "surah_id", Value: a.SurahID}, {Key: "ayah_number", Value: a.AyahNumber}}).
			SetUpdate(bson.D{{Key: "$set", Value: a}}).
			SetUpsert(true))
	}

	res, err := col.BulkWrite(ctx, ops)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("upserted %d placeholder ayahs — replace with real verified corpus data\n", int(res.MatchedCount+res.UpsertedCount))

	_, _ = col.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "surah_id", Value: 1}, {Key: "ayah_number", Value: 1}}})
}
