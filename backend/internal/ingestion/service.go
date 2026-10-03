package ingestion

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"

	"tadbor/backend/internal/embedding"
)

// Default pipeline behaviour. The zero value of IngestOptions means "verified
// production defaults", so callers only state what they want to change.
type IngestOptions struct {
	// EmbeddingModel defaults to embedding.ModelID. Storing the model on every
	// chunk is what lets a future model change be detected rather than silently
	// mixing two embedding spaces in one collection.
	EmbeddingModel string
	// BatchSize defaults to embedding.DefaultMaxBatchSize.
	BatchSize int
	// MaxChunkRunes defaults to DefaultMaxChunkRunes.
	MaxChunkRunes int
	// PruneStale deletes stored chunks for this (source, source version) that
	// the manifest no longer contains. On by default from the runner; opt-in
	// here so a caller can preview a run without mutating the index.
	PruneStale bool
	// DryRun performs the gate, chunking, validation, and resume accounting but
	// makes no provider calls and writes nothing.
	DryRun bool
}

// IngestReport is the audit record of one run: what the manifest contained, what
// was reused, what cost provider calls, and what changed in the index.
type IngestReport struct {
	SourceID      string `json:"source_id"`
	SourceVersion string `json:"source_version"`
	TotalChunks   int    `json:"total_chunks"`
	Reused        int    `json:"reused"`
	Pending       int    `json:"pending"`
	PlannedCalls  int    `json:"planned_calls"`
	Embedded      int    `json:"embedded"`
	EmbedCalls    int    `json:"embed_calls"`
	Written       int    `json:"written"`
	Deleted       int    `json:"deleted"`
	DryRun        bool   `json:"dry_run"`
}

// Service is the IngestionService of docs/rag-architecture-part2.md §25: an
// internal, offline module. It is intentionally not mounted on the HTTP API.
//
// The only interface it depends on is the embedding provider, because that is
// the only external boundary where a test double buys something; MongoDB is
// handled directly, matching every other service in this codebase.
type Service struct {
	embedder embedding.Embedder
	store    *Store
	registry *SourceRegistry
	chunker  *Chunker
	batch    int
}

func NewService(db *mongo.Database, e embedding.Embedder) *Service {
	return &Service{
		embedder: e,
		store:    NewStore(db),
		registry: NewSourceRegistry(db),
		chunker:  NewChunker(DefaultMaxChunkRunes),
		batch:    embedding.DefaultMaxBatchSize,
	}
}

// IngestSource runs Part 1 §7's pipeline for one source over the supplied
// manifest: gate → normalize → verse-anchored chunk → validate → deterministic
// ids → batched embed → vector validation → idempotent upsert → prune.
//
// Order is load-bearing. The usability gate and chunk validation both run before
// the first provider call, so unusable or malformed content never costs a single
// embedding.
func (s *Service) IngestSource(ctx context.Context, sourceID string, docs []Document, opts IngestOptions) (IngestReport, error) {
	rep := IngestReport{SourceID: sourceID, DryRun: opts.DryRun}

	src, err := s.registry.Get(ctx, sourceID)
	if err != nil {
		return rep, fmt.Errorf("load source %s: %w", sourceID, err)
	}

	// The gate is the single authority on whether this source's content may ever
	// reach the LLM (Part 1 §6/§12). It is checked first and separately from
	// chunk validation so an unapproved source is refused even with a valid
	// manifest.
	usable, err := s.registry.IsUsable(ctx, sourceID)
	if err != nil {
		return rep, fmt.Errorf("check source %s: %w", sourceID, err)
	}
	if !usable {
		return rep, fmt.Errorf("source %s: %w", sourceID, ErrSourceNotUsable)
	}

	if err := s.store.EnsureIndexes(ctx); err != nil {
		return rep, fmt.Errorf("ensure chunk indexes: %w", err)
	}

	if len(docs) == 0 {
		// Nothing supplied is a completed no-op, not a failure: this is the
		// expected state before a real corpus exists.
		return rep, nil
	}

	chunker := s.chunker
	if opts.MaxChunkRunes > 0 {
		chunker = NewChunker(opts.MaxChunkRunes)
	}

	var (
		chunks   []Chunk
		versions = make(map[string]bool)
	)
	for _, doc := range docs {
		if doc.SourceVersion == "" {
			doc.SourceVersion = src.Edition
		}
		if doc.Language == "" {
			doc.Language = src.Language
		}
		if doc.ContentType == "" {
			doc.ContentType = ContentTypeTafsir
		}

		built, err := chunker.Build(doc, *src)
		if err != nil {
			return rep, fmt.Errorf("document %s: %w", doc.ID, err)
		}
		for i := range built {
			if err := ValidateChunk(built[i]); err != nil {
				return rep, err
			}
		}
		versions[doc.SourceVersion] = true
		chunks = append(chunks, built...)
	}
	if len(versions) != 1 {
		return rep, fmt.Errorf("manifest mixes source versions %v; ingest one edition at a time so the manifest diff stays well-defined", keysOf(versions))
	}

	var sourceVersion string
	for v := range versions {
		sourceVersion = v
	}
	rep.SourceVersion = sourceVersion
	rep.TotalChunks = len(chunks)

	model := modelOrDefault(opts.EmbeddingModel)
	batchSize := s.batch
	if opts.BatchSize > 0 {
		batchSize = opts.BatchSize
	}

	existing, err := s.store.Existing(ctx, src.ID, sourceVersion)
	if err != nil {
		return rep, fmt.Errorf("read existing chunks: %w", err)
	}

	// Reuse is decided by chunk id (which already encodes the content hash) plus
	// the stored model. An existing id means the text is unchanged; a different
	// model means the stored vector is no longer trustworthy for this corpus.
	keep := make([]string, 0, len(chunks))
	var pending []Chunk
	for _, c := range chunks {
		keep = append(keep, c.ID)
		if prev, ok := existing[c.ID]; ok && prev.EmbeddingModel == model {
			rep.Reused++
			continue
		}
		pending = append(pending, c)
	}

	// PlannedCalls is what this run would cost, so a dry run can be used to
	// check a manifest before spending anything.
	rep.Pending = len(pending)
	rep.PlannedCalls = (len(pending) + batchSize - 1) / batchSize

	if opts.DryRun {
		return rep, nil
	}
	if len(pending) == 0 {
		if err := s.finish(ctx, src.ID, sourceVersion, keep, opts.PruneStale, &rep); err != nil {
			return rep, err
		}
		return rep, nil
	}

	if s.embedder == nil {
		return rep, errors.New("no embedder configured; only a dry run can run without one")
	}
	if err := s.registry.SetIngestionStatus(ctx, sourceID, StatusInProgress); err != nil {
		return rep, fmt.Errorf("mark source in progress: %w", err)
	}

	var embedded []Chunk
	for start := 0; start < len(pending); start += batchSize {
		end := min(start+batchSize, len(pending))
		window := pending[start:end]

		texts := make([]string, len(window))
		for i, c := range window {
			texts[i] = c.Text
		}

		rep.EmbedCalls++
		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			_ = s.registry.SetIngestionStatus(ctx, sourceID, StatusFailed)
			return rep, fmt.Errorf("embed chunks [%d:%d]: %w", start, end, err)
		}
		if err := ValidateBatch(window, vectors); err != nil {
			_ = s.registry.SetIngestionStatus(ctx, sourceID, StatusFailed)
			return rep, err
		}
		for i := range window {
			window[i].embed(vectors[i], model)
		}
		embedded = append(embedded, window...)
	}
	rep.Embedded = len(embedded)

	res, err := s.store.UpsertChunks(ctx, embedded)
	if err != nil {
		_ = s.registry.SetIngestionStatus(ctx, sourceID, StatusFailed)
		return rep, fmt.Errorf("upsert chunks: %w", err)
	}
	rep.Written = int(res.MatchedCount + res.UpsertedCount)

	if err := s.finish(ctx, src.ID, sourceVersion, keep, opts.PruneStale, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// finish prunes superseded chunks and marks the source ingested.
func (s *Service) finish(ctx context.Context, sourceID, sourceVersion string, keep []string, prune bool, rep *IngestReport) error {
	if prune {
		deleted, err := s.store.DeleteStale(ctx, sourceID, sourceVersion, keep)
		if err != nil {
			_ = s.registry.SetIngestionStatus(ctx, sourceID, StatusFailed)
			return fmt.Errorf("delete stale chunks: %w", err)
		}
		rep.Deleted = int(deleted)
	}
	if err := s.registry.MarkIngested(ctx, sourceID, timestamp()); err != nil {
		return fmt.Errorf("mark source ingested: %w", err)
	}
	return nil
}

// Revalidate re-applies the registry gate to every chunk already stored for a
// source. Chunks denormalize source_verified at ingest time, so this is how a
// later downgrade or licensing revocation takes effect without re-embedding the
// whole source.
func (s *Service) Revalidate(ctx context.Context, sourceID string) (int64, error) {
	src, err := s.registry.Get(ctx, sourceID)
	if err != nil {
		return 0, fmt.Errorf("load source %s: %w", sourceID, err)
	}
	updated, err := s.store.SyncSourceState(ctx, *src)
	if err != nil {
		return 0, fmt.Errorf("sync source state: %w", err)
	}
	return updated, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
