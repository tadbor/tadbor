package review

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"tadbor/backend/internal/ingestion"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrIntroductionNotPending = errors.New("introduction is not pending review")

func (s *Service) PendingIntroductions(ctx context.Context) ([]IntroductionQueueItem, error) {
	cur, err := s.introductions.Find(ctx, bson.M{"review_status": "pending"},
		options.Find().SetSort(bson.D{{Key: "surah_id", Value: 1}, {Key: "style", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var items []IntroductionQueueItem
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []IntroductionQueueItem{}, nil
	}

	refs, seen := []string{}, map[string]bool{}
	for _, item := range items {
		for _, ref := range item.SourceRefs {
			if ref != "" && !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	byID := map[string]IntroductionEvidence{}
	if len(refs) > 0 {
		chunkCursor, err := s.chunks.Find(ctx, bson.M{"_id": bson.M{"$in": refs}})
		if err != nil {
			return nil, err
		}
		var chunks []IntroductionEvidence
		if err := chunkCursor.All(ctx, &chunks); err != nil {
			_ = chunkCursor.Close(ctx)
			return nil, err
		}
		_ = chunkCursor.Close(ctx)
		for _, chunk := range chunks {
			byID[chunk.ChunkID] = chunk
		}
	}
	sourceIDs := []string{}
	sourceSeen := map[string]bool{}
	for _, chunk := range byID {
		if chunk.SourceID != "" && !sourceSeen[chunk.SourceID] {
			sourceSeen[chunk.SourceID] = true
			sourceIDs = append(sourceIDs, chunk.SourceID)
		}
	}
	sourceNames := map[string]struct{ Title, Author, Edition string }{}
	if len(sourceIDs) > 0 {
		sourceCursor, err := s.sources.Find(ctx, bson.M{"_id": bson.M{"$in": sourceIDs}})
		if err != nil {
			return nil, err
		}
		var sources []struct {
			ID      string `bson:"_id"`
			Title   string `bson:"title"`
			Author  string `bson:"author"`
			Edition string `bson:"edition"`
		}
		if err := sourceCursor.All(ctx, &sources); err != nil {
			_ = sourceCursor.Close(ctx)
			return nil, err
		}
		_ = sourceCursor.Close(ctx)
		for _, source := range sources {
			sourceNames[source.ID] = struct{ Title, Author, Edition string }{source.Title, source.Author, source.Edition}
		}
	}
	for i := range items {
		for _, ref := range items[i].SourceRefs {
			chunk, ok := byID[ref]
			if !ok {
				items[i].MissingRefs = append(items[i].MissingRefs, ref)
				continue
			}
			items[i].Evidence = append(items[i].Evidence, chunk)
		}
		if len(items[i].MissingRefs) > 0 {
			items[i].ReviewBlockReason = "one or more cited evidence chunks are missing"
		}
		for evidenceIndex := range items[i].Evidence {
			source := sourceNames[items[i].Evidence[evidenceIndex].SourceID]
			items[i].Evidence[evidenceIndex].SourceTitle = source.Title
			items[i].Evidence[evidenceIndex].SourceAuthor = source.Author
			items[i].Evidence[evidenceIndex].SourceEdition = source.Edition
		}
		if items[i].ReviewBlockReason == "" {
			if err := s.validateIntroductionForPublication(ctx, items[i]); err != nil {
				items[i].ReviewBlockReason = err.Error()
			}
		}
	}
	return items, nil
}

func (s *Service) DecideIntroduction(ctx context.Context, id, reviewerID, decision, comments string, checklist IntroductionChecklist) error {
	if strings.TrimSpace(reviewerID) == "" {
		return fmt.Errorf("%w: reviewer_id is required", ErrBadDecision)
	}
	if decision != "approve" && decision != "reject" && decision != "escalate" {
		return fmt.Errorf("%w: introduction decision must be approve, reject, or escalate", ErrBadDecision)
	}
	if decision == "approve" && !checklist.Complete() {
		return fmt.Errorf("%w: all introduction checklist items are required", ErrBadDecision)
	}
	var item IntroductionQueueItem
	if err := s.introductions.FindOne(ctx, bson.M{"_id": id}).Decode(&item); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("%w: no introduction with id %q", ErrBadDecision, id)
		}
		return err
	}
	if item.ReviewStatus != "pending" {
		return ErrIntroductionNotPending
	}
	if decision == "approve" {
		if err := s.validateIntroductionForPublication(ctx, item); err != nil {
			return fmt.Errorf("%w: %v", ErrBadDecision, err)
		}
	}

	nextStatus := map[string]string{"approve": "published", "reject": "rejected", "escalate": "pending"}[decision]
	if _, err := s.reviews.InsertOne(ctx, ReviewRecord{
		IntroductionID: id, ContentType: "surah_introduction", ReviewerID: reviewerID,
		Decision: decision, Comments: comments, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		SourceVersion: item.SourceVersionSnapshot, PromptVersion: item.PromptVersion, ModelVersion: item.ModelVersion,
	}); err != nil {
		return err
	}
	result, err := s.introductions.UpdateOne(ctx,
		bson.M{"_id": id, "review_status": "pending"},
		bson.M{"$set": bson.M{"review_status": nextStatus, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrIntroductionNotPending
	}
	return nil
}

func (s *Service) validateIntroductionForPublication(ctx context.Context, item IntroductionQueueItem) error {
	if item.SurahID != 12 || len(item.Sections) == 0 || len(item.SourceRefs) == 0 {
		return errors.New("introduction has no pilot surah, sections, or source refs")
	}
	knownRefs := map[string]bool{}
	for _, ref := range item.SourceRefs {
		knownRefs[ref] = true
	}
	for _, section := range item.Sections {
		if len(section.Claims) == 0 {
			return errors.New("section has no claim-level citations")
		}
		for _, claim := range section.Claims {
			if strings.TrimSpace(claim.Text) == "" || len(claim.SourceRefs) == 0 {
				return errors.New("section contains an empty or uncited claim")
			}
			for _, ref := range claim.SourceRefs {
				if !knownRefs[ref] {
					return fmt.Errorf("claim ref %q is absent from introduction source_refs", ref)
				}
			}
		}
	}
	for _, claim := range item.DifferingReports {
		if strings.TrimSpace(claim.Text) == "" || len(claim.SourceRefs) == 0 {
			return errors.New("differing report is empty or uncited")
		}
		for _, ref := range claim.SourceRefs {
			if !knownRefs[ref] {
				return fmt.Errorf("differing report ref %q is absent from introduction source_refs", ref)
			}
		}
	}
	if len(item.MissingRefs) > 0 {
		return errors.New("one or more cited evidence chunks are missing")
	}

	var chunks []IntroductionEvidence
	cur, err := s.chunks.Find(ctx, bson.M{"_id": bson.M{"$in": item.SourceRefs}, "source_verified": true})
	if err != nil {
		return err
	}
	if err := cur.All(ctx, &chunks); err != nil {
		_ = cur.Close(ctx)
		return err
	}
	_ = cur.Close(ctx)
	if len(chunks) != len(uniqueRefs(item.SourceRefs)) {
		return errors.New("source refs do not all resolve to verified chunks")
	}
	versionsBySource := map[string]string{}
	versions := make([]string, 0, len(chunks))
	registry := ingestion.NewSourceRegistry(s.introductions.Database())
	for _, chunk := range chunks {
		usable, err := registry.IsUsable(ctx, chunk.SourceID)
		if err != nil {
			return err
		}
		if !usable {
			return fmt.Errorf("source %q is no longer verified and rights-cleared", chunk.SourceID)
		}
		version, err := registry.Version(ctx, chunk.SourceID)
		if err != nil {
			return err
		}
		if chunk.SourceVersion != version.ID || chunk.SourceVersionHash != version.ContentHash {
			return fmt.Errorf("chunk %q is not from its source's current pinned version", chunk.ChunkID)
		}
		versionsBySource[chunk.SourceID] = chunk.SourceVersion
	}
	for _, version := range versionsBySource {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	if strings.Join(uniqueRefs(versions), "+") != item.SourceVersionSnapshot {
		return errors.New("introduction source version snapshot is stale")
	}
	return nil
}

func uniqueRefs(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
