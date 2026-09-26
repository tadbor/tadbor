package ingestion

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Source mirrors the "sources" collection — see docs/rag-architecture-part1.md §6.
type Source struct {
	ID                 string `bson:"_id,omitempty" json:"id"`
	Title              string `bson:"title" json:"title"`
	Author             string `bson:"author" json:"author"`
	Language           string `bson:"language" json:"language"`
	AuthorityTier      int    `bson:"authority_tier" json:"authority_tier"` // 1, 2, or 3
	LicensingStatus    string `bson:"licensing_status" json:"licensing_status"`
	VerificationStatus string `bson:"verification_status" json:"verification_status"` // unverified | verified
}

type SourceRegistry struct {
	sources *mongo.Collection
}

func NewSourceRegistry(db *mongo.Database) *SourceRegistry {
	return &SourceRegistry{sources: db.Collection("sources")}
}

// IsUsable is the single gate every retrieval/generation query should check
// (directly or via the "source_verified" denormalized flag on chunks) before
// a source's content can ever reach the LLM.
func (r *SourceRegistry) IsUsable(ctx context.Context, sourceID string) (bool, error) {
	var src Source
	err := r.sources.FindOne(ctx, bson.M{"_id": sourceID}).Decode(&src)
	if err != nil {
		return false, err
	}
	return src.VerificationStatus == "verified" && src.LicensingStatus == "cleared", nil
}
