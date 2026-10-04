package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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

	// CurrentVersion names the source_versions document this source's content
	// should be ingested as. It is the pointer that makes a snapshot explicit:
	// without it, a source with two registered snapshots is ambiguous, and
	// keying chunks off Edition silently conflates them.
	CurrentVersion string `bson:"current_version,omitempty" json:"current_version,omitempty"`
}

// SourceVersion mirrors the "source_versions" collection (Part 2 §24). It is the
// identity of one digitised snapshot of a work, as distinct from the work itself.
//
// This is deliberately separate from Source. A Source is a work — Ibn Kathir's
// Tafsir — and stays one document no matter how many times it is re-digitised.
// A SourceVersion is one specific snapshot of it, pinned by ContentHash. Chunks
// are keyed by SourceVersion.ID, not by Source.Edition, so that:
//
//   - two snapshots of the same edition cannot overwrite each other, because
//     they carry different version ids;
//   - a chunk can be traced to the exact bytes it was built from, because the
//     version carries the hash and the chunk carries that hash forward.
type SourceVersion struct {
	ID           string `bson:"_id,omitempty" json:"id"`
	SourceID     string `bson:"source_id" json:"source_id"`
	Edition      string `bson:"edition" json:"edition"`
	VersionLabel string `bson:"version_label" json:"version_label"`
	RawTextRef   string `bson:"raw_text_ref,omitempty" json:"raw_text_ref,omitempty"`
	ContentHash  string `bson:"content_hash" json:"content_hash"`
}

type SourceRegistry struct {
	sources  *mongo.Collection
	versions *mongo.Collection
}

func NewSourceRegistry(db *mongo.Database) *SourceRegistry {
	return &SourceRegistry{
		sources:  db.Collection("sources"),
		versions: db.Collection("source_versions"),
	}
}

// ErrNoPinnedVersion reports a source with no registered snapshot. It is an
// error rather than a fallback to Edition, because falling back is the bug this
// replaced: Edition identifies an edition, not a snapshot, and nothing would
// record which bytes were actually ingested.
var ErrNoPinnedVersion = errors.New("source has no registered source_version")

// Version returns the snapshot a source's content must be ingested as.
//
// sources.current_version is authoritative. A source with exactly one registered
// version and no pointer is accepted, because that is the state issue #3 left the
// registry in and it is unambiguous. A source with several versions and no
// pointer is refused: choosing one silently would ingest the wrong snapshot.
func (r *SourceRegistry) Version(ctx context.Context, sourceID string) (*SourceVersion, error) {
	var src Source
	if err := r.sources.FindOne(ctx, bson.M{"_id": sourceID}).Decode(&src); err != nil {
		return nil, err
	}

	if src.CurrentVersion != "" {
		var v SourceVersion
		if err := r.versions.FindOne(ctx, bson.M{"_id": src.CurrentVersion}).Decode(&v); err != nil {
			return nil, fmt.Errorf("source %s points at version %s: %w", sourceID, src.CurrentVersion, err)
		}
		if v.SourceID != sourceID {
			return nil, fmt.Errorf("version %s belongs to source %q, not %q", v.ID, v.SourceID, sourceID)
		}
		if v.ContentHash == "" {
			return nil, fmt.Errorf("source_versions %s has no content_hash, so the snapshot is not pinned", v.ID)
		}
		return &v, nil
	}

	cur, err := r.versions.Find(ctx, bson.M{"source_id": sourceID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var all []SourceVersion
	if err := cur.All(ctx, &all); err != nil {
		return nil, err
	}
	switch len(all) {
	case 0:
		return nil, fmt.Errorf("%w: %s", ErrNoPinnedVersion, sourceID)
	case 1:
		if all[0].ContentHash == "" {
			return nil, fmt.Errorf("source_versions %s has no content_hash, so the snapshot is not pinned", all[0].ID)
		}
		return &all[0], nil
	default:
		ids := make([]string, 0, len(all))
		for _, v := range all {
			ids = append(ids, v.ID)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("source %s has %d registered versions (%s) and no sources.current_version pointer; "+
			"refusing to guess which snapshot to ingest", sourceID, len(all), strings.Join(ids, ", "))
	}
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
