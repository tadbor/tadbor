package ingestion

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Source state vocabulary. These are the exact values SourceRegistry.IsUsable
// compares against — the gate's semantics are unchanged from the original
// implementation, they are simply named now that the ingestion pipeline also
// reads and writes source documents.
const (
	VerificationVerified = "verified"
	LicensingCleared     = "cleared"
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

	// Added by the ingestion pipeline: descriptive registry fields from §6 that
	// are not part of the usability gate.
	Edition                   string `bson:"edition,omitempty" json:"edition,omitempty"`
	MethodologyClassification string `bson:"methodology_classification,omitempty" json:"methodology_classification,omitempty"`
	Metadata                  bson.M `bson:"metadata,omitempty" json:"metadata,omitempty"`

	// IngestionStatus tracks how far the source has moved through the pipeline
	// in §7 (not_started | in_progress | ingested | failed). It is pipeline
	// bookkeeping only — never a substitute for VerificationStatus/LicensingStatus
	// when deciding whether content may be used.
	IngestionStatus string `bson:"ingestion_status,omitempty" json:"ingestion_status,omitempty"`
	IngestedAt      string `bson:"ingested_at,omitempty" json:"ingested_at,omitempty"`
	RawTextRef      string `bson:"raw_text_ref,omitempty" json:"raw_text_ref,omitempty"`
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
	return src.VerificationStatus == VerificationVerified && src.LicensingStatus == LicensingCleared, nil
}

// Get returns a source registry entry. The ingestion pipeline needs the full
// record — not just the gate verdict — because every chunk denormalizes the
// source's authority tier and review status (Part 1 §10).
func (r *SourceRegistry) Get(ctx context.Context, sourceID string) (*Source, error) {
	var src Source
	if err := r.sources.FindOne(ctx, bson.M{"_id": sourceID}).Decode(&src); err != nil {
		return nil, err
	}
	return &src, nil
}

// List returns all sources ordered by id, for the offline runner's inventory view.
func (r *SourceRegistry) List(ctx context.Context) ([]Source, error) {
	cur, err := r.sources.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []Source
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetIngestionStatus records pipeline progress for a source so an interrupted
// run stays visible and resumable in the registry.
func (r *SourceRegistry) SetIngestionStatus(ctx context.Context, sourceID, status string) error {
	_, err := r.sources.UpdateOne(ctx, bson.M{"_id": sourceID}, bson.M{
		"$set": bson.M{"ingestion_status": status},
	})
	return err
}

// MarkIngested records a completed run, including when it completed.
func (r *SourceRegistry) MarkIngested(ctx context.Context, sourceID, at string) error {
	_, err := r.sources.UpdateOne(ctx, bson.M{"_id": sourceID}, bson.M{
		"$set": bson.M{"ingestion_status": StatusIngested, "ingested_at": at},
	})
	return err
}
