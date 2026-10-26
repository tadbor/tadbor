package content

import (
	"context"
	"errors"
	"fmt"

	"tadbor/backend/internal/ingestion"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrIntroductionUnavailable = errors.New("introduction unavailable")
	ErrIntroductionProcessing  = errors.New("introduction processing")
)

type IntroductionRepository interface {
	FindPublished(context.Context, int, string) (*SurahIntroduction, error)
	HasUnpublished(context.Context, int, string) (bool, error)
	FindAttributions(context.Context, []string) ([]SourceAttribution, error)
}

type Service struct {
	explanations  *mongo.Collection
	introductions IntroductionRepository
}

func NewService(db *mongo.Database) *Service {
	return &Service{
		explanations: db.Collection("explanations"),
		introductions: mongoIntroductionRepository{
			introductions: db.Collection("surah_introductions"),
			chunks:        db.Collection("tafsir_chunks"),
			sources:       db.Collection("sources"),
		},
	}
}

func NewServiceWithIntroductionRepository(repo IntroductionRepository) *Service {
	return &Service{introductions: repo}
}

// GetExplanations returns only published explanations for a surah in a given style.
// This hard filter is the enforcement point for "reader never sees unreviewed content."
func (s *Service) GetExplanations(ctx context.Context, surahID int, style string) ([]Explanation, error) {
	filter := bson.M{
		"surah_id":      surahID,
		"style":         style,
		"review_status": "published",
	}
	cur, err := s.explanations.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []Explanation
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) GetIntroduction(ctx context.Context, surahID int, style string) (*SurahIntroduction, error) {
	intro, err := s.introductions.FindPublished(ctx, surahID, style)
	if errors.Is(err, mongo.ErrNoDocuments) {
		pending, pendingErr := s.introductions.HasUnpublished(ctx, surahID, style)
		if pendingErr != nil {
			return nil, pendingErr
		}
		if pending {
			return nil, ErrIntroductionProcessing
		}
		return nil, ErrIntroductionUnavailable
	}
	if err != nil {
		return nil, err
	}
	if len(intro.SourceRefs) == 0 {
		return nil, errors.New("published introduction has no source_refs")
	}
	intro.Attributions, err = s.introductions.FindAttributions(ctx, intro.SourceRefs)
	if err != nil {
		return nil, fmt.Errorf("resolve introduction attribution: %w", err)
	}
	if len(intro.Attributions) == 0 {
		return nil, errors.New("published introduction has no source attribution")
	}
	return intro, nil
}

type mongoIntroductionRepository struct {
	introductions *mongo.Collection
	chunks        *mongo.Collection
	sources       *mongo.Collection
}

func publishedIntroductionFilter(surahID int, style string) bson.M {
	return bson.M{"surah_id": surahID, "style": style, "review_status": "published"}
}

func unpublishedIntroductionFilter(surahID int, style string) bson.M {
	return bson.M{"surah_id": surahID, "style": style, "review_status": bson.M{"$in": []string{"pending", "processing"}}}
}

func (r mongoIntroductionRepository) FindPublished(ctx context.Context, surahID int, style string) (*SurahIntroduction, error) {
	var intro SurahIntroduction
	if err := r.introductions.FindOne(ctx, publishedIntroductionFilter(surahID, style)).Decode(&intro); err != nil {
		return nil, err
	}
	return &intro, nil
}

func (r mongoIntroductionRepository) HasUnpublished(ctx context.Context, surahID int, style string) (bool, error) {
	count, err := r.introductions.CountDocuments(ctx, unpublishedIntroductionFilter(surahID, style))
	return count > 0, err
}

func (r mongoIntroductionRepository) FindAttributions(ctx context.Context, refs []string) ([]SourceAttribution, error) {
	if len(refs) == 0 {
		return []SourceAttribution{}, nil
	}

	chunkCursor, err := r.chunks.Find(ctx, bson.M{"_id": bson.M{"$in": refs}})
	if err != nil {
		return nil, err
	}
	defer chunkCursor.Close(ctx)
	var chunks []struct {
		ID                string `bson:"_id"`
		SourceID          string `bson:"source_id"`
		SourceVersion     string `bson:"source_version"`
		SourceVersionHash string `bson:"source_version_hash"`
	}
	if err := chunkCursor.All(ctx, &chunks); err != nil {
		return nil, err
	}
	chunkByID := make(map[string]struct {
		SourceID          string
		SourceVersion     string
		SourceVersionHash string
	}, len(chunks))
	sourceIDs := make([]string, 0, len(chunks))
	seenSources := map[string]bool{}
	for _, chunk := range chunks {
		chunkByID[chunk.ID] = struct {
			SourceID          string
			SourceVersion     string
			SourceVersionHash string
		}{chunk.SourceID, chunk.SourceVersion, chunk.SourceVersionHash}
		if chunk.SourceID != "" && !seenSources[chunk.SourceID] {
			seenSources[chunk.SourceID] = true
			sourceIDs = append(sourceIDs, chunk.SourceID)
		}
	}
	for _, ref := range refs {
		if _, ok := chunkByID[ref]; !ok {
			return nil, fmt.Errorf("source ref %q does not resolve to a chunk", ref)
		}
	}
	if len(sourceIDs) == 0 {
		return nil, errors.New("introduction refs have no registered source")
	}

	sourceCursor, err := r.sources.Find(ctx, bson.M{"_id": bson.M{"$in": sourceIDs}})
	if err != nil {
		return nil, err
	}
	defer sourceCursor.Close(ctx)
	var sources []struct {
		ID      string `bson:"_id"`
		Title   string `bson:"title"`
		Author  string `bson:"author"`
		Edition string `bson:"edition,omitempty"`
	}
	if err := sourceCursor.All(ctx, &sources); err != nil {
		return nil, err
	}
	sourceByID := make(map[string]struct {
		Title   string
		Author  string
		Edition string
	}, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = struct {
			Title   string
			Author  string
			Edition string
		}{source.Title, source.Author, source.Edition}
	}

	attributions := make([]SourceAttribution, 0, len(sourceIDs))
	indexBySource := map[string]int{}
	registry := ingestion.NewSourceRegistry(r.sources.Database())
	for _, ref := range refs {
		chunk := chunkByID[ref]
		source, ok := sourceByID[chunk.SourceID]
		if !ok {
			return nil, fmt.Errorf("source %q for ref %q is not registered", chunk.SourceID, ref)
		}
		usable, err := registry.IsUsable(ctx, chunk.SourceID)
		if err != nil {
			return nil, err
		}
		if !usable {
			return nil, fmt.Errorf("source %q is no longer verified and rights-cleared", chunk.SourceID)
		}
		version, err := registry.Version(ctx, chunk.SourceID)
		if err != nil {
			return nil, err
		}
		if chunk.SourceVersion != version.ID || chunk.SourceVersionHash != version.ContentHash {
			return nil, fmt.Errorf("source ref %q does not match the current pinned source version", ref)
		}
		index, exists := indexBySource[chunk.SourceID]
		if !exists {
			index = len(attributions)
			indexBySource[chunk.SourceID] = index
			attributions = append(attributions, SourceAttribution{
				SourceID: chunk.SourceID, Title: source.Title, Author: source.Author,
				Edition: source.Edition, SourceVersion: chunk.SourceVersion,
				SourceRefs: []string{},
			})
		}
		attributions[index].SourceRefs = append(attributions[index].SourceRefs, ref)
	}
	return attributions, nil
}
