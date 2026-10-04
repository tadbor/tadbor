package ingestion

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AyahMapping is one row of the mapping table from Part 1 §8 — the structure
// retrieval treats as authoritative for "which chunk speaks about this verse".
//
// The same four fields are denormalized onto the chunk itself, because
// internal/retrieval projects them directly and cannot join back to a second
// collection on the hot path. The mapping collection exists anyway, for two
// reasons the denormalized copy cannot serve:
//
//   - §8 names it as the thing to query when auditing coverage of a surah, and
//     issue #5's acceptance criterion is written directly against it.
//   - It is the narrower document. Answering "is every ayah covered, and by
//     chunks from which source versions" does not require loading chunk text.
//
// The two must not drift. Mappings are derived from the same []Chunk slice that
// produces the chunks, in the same write step, so a chunk cannot exist without
// its mapping and no mapping can exist without its chunk. VerifyMappingsConsistent
// re-checks that after the fact, which matters because nothing in the schema
// prevents a future writer from updating one side only.
//
// _id is the chunk id: §8 gives a chunk exactly one mapping, so this enforces
// "no chunk is mapped twice" as a uniqueness constraint rather than a convention.
type AyahMapping struct {
	ID            string `bson:"_id" json:"id"`
	ChunkID       string `bson:"chunk_id" json:"chunk_id"`
	SourceID      string `bson:"source_id" json:"source_id"`
	SourceVersion string `bson:"source_version" json:"source_version"`
	SurahID       int    `bson:"surah_id" json:"surah_id"`
	AyahStart     int    `bson:"ayah_start" json:"ayah_start"`
	AyahEnd       int    `bson:"ayah_end" json:"ayah_end"`
	MappingType   string `bson:"mapping_type" json:"mapping_type"`
}

// MappingFor returns the mapping a chunk implies. It re-runs the same range
// validation the chunker applied, so a mapping can never be written for a range
// that Part 1 §8 does not permit — a surah_level or thematic chunk claiming
// ayah 3–4 would silently become exact evidence for verse 4.
func MappingFor(c Chunk) (AyahMapping, error) {
	if err := validateMappingRange(c.MappingType, c.AyahStart, c.AyahEnd); err != nil {
		return AyahMapping{}, fmt.Errorf("chunk %s: %w", c.ID, err)
	}
	return AyahMapping{
		ID:            c.ID,
		ChunkID:       c.ID,
		SourceID:      c.SourceID,
		SourceVersion: c.SourceVersion,
		SurahID:       c.SurahID,
		AyahStart:     c.AyahStart,
		AyahEnd:       c.AyahEnd,
		MappingType:   c.MappingType,
	}, nil
}

// MappingsFor derives the mapping rows for a set of chunks, failing on the first
// invalid one rather than writing a partial table.
func MappingsFor(chunks []Chunk) ([]AyahMapping, error) {
	out := make([]AyahMapping, 0, len(chunks))
	for _, c := range chunks {
		m, err := MappingFor(c)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// UpsertMappings writes the mapping rows idempotently. As with chunks, _id is
// the chunk id, so re-running overwrites in place rather than duplicating.
func (s *Store) UpsertMappings(ctx context.Context, mappings []AyahMapping) (*mongo.BulkWriteResult, error) {
	if len(mappings) == 0 {
		return &mongo.BulkWriteResult{}, nil
	}
	ops := make([]mongo.WriteModel, 0, len(mappings))
	for _, m := range mappings {
		raw, err := bson.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("encode mapping %s: %w", m.ID, err)
		}
		var doc bson.M
		if err := bson.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("decode mapping %s: %w", m.ID, err)
		}
		delete(doc, "_id")
		ops = append(ops, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": m.ID}).
			SetUpdate(bson.M{"$set": doc}).
			SetUpsert(true))
	}
	return s.mappings.BulkWrite(ctx, ops, options.BulkWrite().SetOrdered(true))
}

// DeleteStaleMappings removes mapping rows for one (source, source version) whose
// chunks are no longer stored. A mapping left behind after its chunk is pruned
// would make coverage reporting claim a chunk that retrieval cannot serve.
func (s *Store) DeleteStaleMappings(ctx context.Context, sourceID, sourceVersion string, keep []string) (int64, error) {
	if keep == nil {
		keep = []string{}
	}
	res, err := s.mappings.DeleteMany(ctx, bson.M{
		"source_id":      sourceID,
		"source_version": sourceVersion,
		"_id":            bson.M{"$nin": keep},
	})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// MappingCoverage is the per-ayah chunk count used to answer "does every ayah
// have evidence, and is any of it actually usable".
type MappingCoverage struct {
	Ayah int `bson:"ayah"`
	// Chunks counts every mapped chunk, regardless of whether its source is
	// approved. A non-zero count with zero Usable is the difference between
	// "no commentary for this verse" and "commentary exists but is not cleared
	// for use", which call for different fixes.
	Chunks int `bson:"chunks"`
	// Usable counts only chunks whose source_verified snapshot is true.
	Usable int `bson:"usable"`
	// Parents counts distinct whole commentary sections, so a verse split into
	// nine paragraph chunks is not reported as nine independent authorities.
	Parents int `bson:"parents"`
	// MappingTypes records the kinds of mapping found, for the audit view.
	MappingTypes []string `bson:"mapping_types"`
}

// CoverageForSurah reports, for every ayah of a surah, how many mapped chunks
// exist. Ayahs with nothing at all are reported with Chunks == 0 rather than
// omitted, so a gap cannot be mistaken for an ayah nobody asked about.
func (s *Store) CoverageForSurah(ctx context.Context, surah int, ayahCount int) (map[int]MappingCoverage, error) {
	pipeline := bson.A{
		bson.D{{Key: "$match", Value: bson.M{"surah_id": surah}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         s.chunks.Name(),
			"localField":   "_id",
			"foreignField": "_id",
			"as":           "chunk",
		}}},
		// preserveNullAndEmptyArrays is load-bearing. A plain $unwind drops any
		// mapping whose chunk is missing, which would make an orphan mapping
		// invisible here — coverage would look clean while claiming nothing for
		// the ayah, which is the opposite of what this reports on. Keeping the
		// null row lets it be counted (and, if out of range, rejected below);
		// VerifyMappingsConsistent is what names it as an orphan.
		bson.D{{Key: "$unwind", Value: bson.M{"path": "$chunk", "preserveNullAndEmptyArrays": true}}},
		bson.D{{Key: "$group", Value: bson.M{
			"_id":           "$ayah_start",
			"chunks":        bson.M{"$sum": 1},
			"usable":        bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$ifNull": bson.A{"$chunk.source_verified", false}}, 1, 0}}},
			"parents":       bson.M{"$addToSet": bson.M{"$ifNull": bson.A{"$chunk.parent_chunk_id", "$_id"}}},
			"mapping_types": bson.M{"$addToSet": "$mapping_type"},
		}}},
	}

	cur, err := s.mappings.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := make(map[int]MappingCoverage, ayahCount)
	for i := 1; i <= ayahCount; i++ {
		out[i] = MappingCoverage{Ayah: i}
	}
	for cur.Next(ctx) {
		var row struct {
			ID           int      `bson:"_id"`
			Chunks       int      `bson:"chunks"`
			Usable       int      `bson:"usable"`
			Parents      []string `bson:"parents"`
			MappingTypes []string `bson:"mapping_types"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		if row.ID < 1 || row.ID > ayahCount {
			// A mapping pointing outside the surah would be invisible in a
			// per-ayah report while still being served by retrieval.
			return nil, fmt.Errorf("mapping points at ayah %d, outside surah %d (1-%d)", row.ID, surah, ayahCount)
		}
		out[row.ID] = MappingCoverage{
			Ayah:         row.ID,
			Chunks:       row.Chunks,
			Usable:       row.Usable,
			Parents:      len(row.Parents),
			MappingTypes: row.MappingTypes,
		}
	}
	return out, cur.Err()
}

// CoverageDrift describes a disagreement between the mapping table and the chunk
// collection.
type CoverageDrift struct {
	// ChunksWithoutMapping are chunk ids that retrieval will serve but that the
	// mapping table does not account for.
	ChunksWithoutMapping []string `json:"chunks_without_mapping"`
	// MappingsWithoutChunk are mapping rows whose chunk is gone, which would make
	// coverage reporting claim evidence retrieval cannot return.
	MappingsWithoutChunk []string `json:"mappings_without_chunk"`
	// Mismatched are chunk ids whose denormalized verse range disagrees with their
	// mapping row — the failure mode the denormalized copy exists to make possible.
	Mismatched []string `json:"mismatched"`
}

// Empty reports whether the two sides agree completely.
func (d CoverageDrift) Empty() bool {
	return len(d.ChunksWithoutMapping) == 0 && len(d.MappingsWithoutChunk) == 0 && len(d.Mismatched) == 0
}

// VerifyMappingsConsistent compares the mapping table against the chunk
// collection. The write path derives both from one slice, so drift can only come
// from a hand edit, a partial write, or a future code change that updates one side
// — which is exactly why it is worth checking rather than assuming.
func (s *Store) VerifyMappingsConsistent(ctx context.Context, sourceID, sourceVersion string) (CoverageDrift, error) {
	var drift CoverageDrift

	chunks, err := s.chunks.Find(ctx,
		bson.M{"source_id": sourceID, "source_version": sourceVersion},
		options.Find().SetProjection(bson.M{
			"_id": 1, "surah_id": 1, "ayah_start": 1, "ayah_end": 1, "mapping_type": 1,
		}))
	if err != nil {
		return drift, err
	}
	defer chunks.Close(ctx)

	stored := map[string]AyahMapping{}
	for chunks.Next(ctx) {
		var c Chunk
		if err := chunks.Decode(&c); err != nil {
			return drift, err
		}
		stored[c.ID] = AyahMapping{
			ID: c.ID, ChunkID: c.ID, SourceID: c.SourceID, SourceVersion: c.SourceVersion,
			SurahID: c.SurahID, AyahStart: c.AyahStart, AyahEnd: c.AyahEnd, MappingType: c.MappingType,
		}
	}
	if err := chunks.Err(); err != nil {
		return drift, err
	}

	maps, err := s.mappings.Find(ctx,
		bson.M{"source_id": sourceID, "source_version": sourceVersion},
		options.Find().SetProjection(bson.M{
			"_id": 1, "chunk_id": 1, "surah_id": 1, "ayah_start": 1, "ayah_end": 1, "mapping_type": 1,
		}))
	if err != nil {
		return drift, err
	}
	defer maps.Close(ctx)

	for maps.Next(ctx) {
		var m AyahMapping
		if err := maps.Decode(&m); err != nil {
			return drift, err
		}
		chunk, ok := stored[m.ID]
		if !ok {
			drift.MappingsWithoutChunk = append(drift.MappingsWithoutChunk, m.ID)
			continue
		}
		delete(stored, m.ID)
		if chunk.SurahID != m.SurahID || chunk.AyahStart != m.AyahStart ||
			chunk.AyahEnd != m.AyahEnd || chunk.MappingType != m.MappingType {
			drift.Mismatched = append(drift.Mismatched, m.ID)
		}
	}
	if err := maps.Err(); err != nil {
		return drift, err
	}

	for id := range stored {
		drift.ChunksWithoutMapping = append(drift.ChunksWithoutMapping, id)
	}
	return drift, nil
}
