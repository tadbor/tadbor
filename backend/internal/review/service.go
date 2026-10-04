package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// ErrNotPending is returned when a decision targets an explanation that is no
	// longer awaiting review. The dashboard submits from a browser form, so a
	// double click or a stale tab is routine; the second submission must neither
	// overwrite the first decision nor add a record claiming it applied.
	ErrNotPending = errors.New("explanation is not pending review")

	// ErrBadDecision is returned for input the reviewer needs to fix: an unknown
	// decision, a missing edited text, an empty reviewer, a missing explanation.
	// The handler maps it to 400 so the dashboard can show what to change.
	ErrBadDecision = errors.New("invalid decision")
)

type Service struct {
	explanations *mongo.Collection
	reviews      *mongo.Collection
	chunks       *mongo.Collection
	ayahs        *mongo.Collection
	sources      *mongo.Collection
}

func NewService(db *mongo.Database) *Service {
	return &Service{
		explanations: db.Collection("explanations"),
		reviews:      db.Collection("reviews"),
		chunks:       db.Collection("tafsir_chunks"),
		ayahs:        db.Collection("ayahs"),
		sources:      db.Collection("sources"),
	}
}

// PendingQueue returns explanations awaiting a reviewer decision, each with its
// evidence resolved and its verse attached.
//
// A reviewer can only check the two questions that matter — "does this text say
// what the source says" and "did it lose or add anything" — if the source text
// and the verse are both in front of them. Returning raw explanations with a
// list of chunk IDs would make the reviewer reconstruct that by hand, which is
// exactly the step that gets skipped.
func (s *Service) PendingQueue(ctx context.Context) ([]QueueItem, error) {
	cur, err := s.explanations.Find(ctx,
		bson.M{"review_status": "pending"},
		options.Find().SetSort(bson.D{{Key: "surah_id", Value: 1}, {Key: "ayah_start", Value: 1}, {Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var items []QueueItem
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		// An empty JSON array, not null: "nothing is waiting" is a normal state
		// for this endpoint, not an absence, and a client should not have to
		// distinguish the two.
		return []QueueItem{}, nil
	}

	if err := s.attachEvidence(ctx, items); err != nil {
		return nil, err
	}
	if err := s.attachAyahs(ctx, items); err != nil {
		return nil, err
	}
	return items, s.attachCitations(ctx, items)
}

// attachEvidence resolves each item's source_refs into chunk text. Refs are
// resolved in the order cited, and refs that no longer resolve are reported
// rather than dropped: a citation the reviewer cannot open is a broken citation.
func (s *Service) attachEvidence(ctx context.Context, items []QueueItem) error {
	refs := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		for _, ref := range it.SourceRefs {
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	byID := map[string]Evidence{}
	if len(refs) > 0 {
		cur, err := s.chunks.Find(ctx, bson.M{"_id": bson.M{"$in": refs}})
		if err != nil {
			return err
		}
		defer cur.Close(ctx)

		var chunks []Evidence
		if err := cur.All(ctx, &chunks); err != nil {
			return err
		}
		for _, c := range chunks {
			byID[c.ChunkID] = c
		}
	}

	for i := range items {
		for _, ref := range items[i].SourceRefs {
			if ref == "" {
				continue
			}
			if chunk, ok := byID[ref]; ok {
				items[i].Evidence = append(items[i].Evidence, chunk)
			} else {
				items[i].MissingRefs = append(items[i].MissingRefs, ref)
			}
		}
	}
	return nil
}

// attachAyahs pulls the Quranic text from the verified corpus. It reads the
// ayahs collection rather than anything the explanation carries, so a reviewer
// is always comparing against the same immutable text the reader will see.
func (s *Service) attachAyahs(ctx context.Context, items []QueueItem) error {
	surahIDs := make([]int, 0, 1)
	seen := map[int]bool{}
	for _, it := range items {
		if !seen[it.SurahID] {
			seen[it.SurahID] = true
			surahIDs = append(surahIDs, it.SurahID)
		}
	}

	cur, err := s.ayahs.Find(ctx, bson.M{"surah_id": bson.M{"$in": surahIDs}})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)

	var ayahs []struct {
		AyahNumber  int    `bson:"ayah_number"`
		TextUthmani string `bson:"text_uthmani"`
	}
	if err := cur.All(ctx, &ayahs); err != nil {
		return err
	}

	byNumber := map[int]string{}
	for _, a := range ayahs {
		byNumber[a.AyahNumber] = a.TextUthmani
	}

	for i := range items {
		var parts []string
		for n := items[i].AyahStart; n <= items[i].AyahEnd; n++ {
			if text := byNumber[n]; text != "" {
				parts = append(parts, text)
			}
		}
		items[i].AyahText = strings.Join(parts, "\n")
	}
	return nil
}

// attachCitations names the source behind the evidence, and flags the case
// Part 1 §20 cares about: the explanation was generated against a source
// version that is no longer the one its evidence came from.
func (s *Service) attachCitations(ctx context.Context, items []QueueItem) error {
	sourceIDs := make([]string, 0, 1)
	seen := map[string]bool{}
	for _, it := range items {
		if len(it.Evidence) == 0 {
			continue
		}
		id := it.Evidence[0].SourceID
		if id != "" && !seen[id] {
			seen[id] = true
			sourceIDs = append(sourceIDs, id)
		}
	}

	byID := map[string]Citation{}
	if len(sourceIDs) > 0 {
		cur, err := s.sources.Find(ctx, bson.M{"_id": bson.M{"$in": sourceIDs}})
		if err != nil {
			return err
		}
		defer cur.Close(ctx)

		var rows []struct {
			ID      string `bson:"_id"`
			Title   string `bson:"title"`
			Author  string `bson:"author"`
			Edition string `bson:"edition"`
		}
		if err := cur.All(ctx, &rows); err != nil {
			return err
		}
		for _, r := range rows {
			byID[r.ID] = Citation{SourceID: r.ID, Title: r.Title, Author: r.Author, Edition: r.Edition}
		}
	}

	for i := range items {
		if len(items[i].Evidence) == 0 {
			continue
		}
		first := items[i].Evidence[0]
		citation := byID[first.SourceID]
		if citation.SourceID == "" {
			citation.SourceID = first.SourceID
		}
		citation.Version = first.SourceVersion
		items[i].Citation = citation

		// A snapshot that disagrees with the evidence is stale grounding. This is
		// the check that would have to become automatic invalidation once the
		// source can change under a published explanation (§20).
		if items[i].SourceVersionSnapshot != "" && first.SourceVersion != "" &&
			items[i].SourceVersionSnapshot != first.SourceVersion {
			items[i].GroundingVersionMismatch = true
		}
	}
	return nil
}

// Decide records a reviewer decision and applies it to the explanation.
//
// The review record is inserted before the status is flipped, and that order is
// deliberate: if the second write fails, the audit trail shows a decision that
// was attempted rather than a status change nobody can account for. For a system
// whose whole point is that no content is published without a human decision, an
// over-recorded decision is recoverable and a silent one is not.
func (s *Service) Decide(ctx context.Context, explanationID, reviewerID, decision, comments, editedText string) error {
	nextStatus, err := statusFor(decision)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reviewerID) == "" {
		return fmt.Errorf("%w: reviewer_id is required", ErrBadDecision)
	}
	if decision == "edit_approve" && strings.TrimSpace(editedText) == "" {
		return fmt.Errorf("%w: edit_approve needs the edited text; use approve to publish the text as generated", ErrBadDecision)
	}

	// Read the explanation first: the review record must name the grounding it
	// was given against (§20), and the status flip must be conditional on the
	// document still being pending.
	var current QueueItem
	if err := s.explanations.FindOne(ctx, bson.M{"_id": explanationID}).Decode(&current); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("%w: no explanation with id %q", ErrBadDecision, explanationID)
		}
		return err
	}
	// Checked before anything is written, so a resubmitted form does not leave a
	// review record claiming a decision that was never applied. The conditional
	// update below is what actually prevents that under concurrency.
	if current.ReviewStatus != "pending" {
		return fmt.Errorf("%w: explanation is not pending review (status %q)", ErrNotPending, current.ReviewStatus)
	}

	set := bson.M{"review_status": nextStatus}
	if decision == "edit_approve" {
		set["text"] = editedText
	}

	record := ReviewRecord{
		ExplanationID: explanationID,
		ReviewerID:    reviewerID,
		Decision:      decision,
		Comments:      comments,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		SourceVersion: current.SourceVersionSnapshot,
		PromptVersion: current.PromptVersion,
		ModelVersion:  current.ModelVersion,
	}
	if _, err := s.reviews.InsertOne(ctx, record); err != nil {
		return err
	}

	res, err := s.explanations.UpdateOne(ctx,
		bson.M{"_id": explanationID, "review_status": "pending"},
		bson.M{"$set": set},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotPending
	}
	return nil
}

// statusFor maps a decision to the review_status it produces. An unrecognised
// decision is an error rather than a no-op: silently leaving a document pending
// while writing a review record that claims it was handled is how content gets
// lost.
func statusFor(decision string) (string, error) {
	switch decision {
	case "approve", "edit_approve":
		return "published", nil
	case "reject":
		return "rejected", nil
	case "escalate":
		// Escalation is not a decision on the text — it asks for a ruling this
		// reviewer is not the right person to make, so the item stays in the queue.
		return "pending", nil
	default:
		return "", fmt.Errorf("%w: unknown decision %q: expected approve, edit_approve, reject or escalate", ErrBadDecision, decision)
	}
}
