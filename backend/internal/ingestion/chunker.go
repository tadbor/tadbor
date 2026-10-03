package ingestion

import "fmt"

// DefaultMaxChunkRunes caps a single chunk's text. Chunks feed an offline LLM
// generation pass, so the limit is about context budget rather than embedding
// limits — bge-m3 handles far longer inputs. Sized in runes because Arabic text
// is roughly two bytes per character.
const DefaultMaxChunkRunes = 1200

// Chunker implements verse-anchored chunking (Part 1 §9): Surah → Ayah → Tafsir
// Section → Paragraph. Fixed-size token windows are explicitly rejected there,
// because splitting one verse's commentary across a window boundary, or merging
// commentary on adjacent verses, destroys the verse mapping that retrieval
// depends on.
type Chunker struct {
	// MaxRunes caps one chunk. Zero means DefaultMaxChunkRunes.
	MaxRunes int
	// Norm produces the retained lexical form. Required.
	Norm *Normalizer
}

// NewChunker returns a chunker with the default size cap.
func NewChunker(maxRunes int) *Chunker {
	if maxRunes <= 0 {
		maxRunes = DefaultMaxChunkRunes
	}
	return &Chunker{MaxRunes: maxRunes, Norm: NewNormalizer()}
}

// Build turns a structurally-detected document into validated chunks.
//
// Chunk identity is derived from (source, source version, position, content), so
// Build is a pure function of its inputs: running it twice over the same document
// yields byte-identical ids. Ordinals are document-global so reading order is
// stable across runs.
//
// Sections keep their own ayah range; the chunker never infers or adjusts verse
// mapping — that is structure detection's job, and inventing it here would be
// exactly the "which verse is this" guessing that Part 1 §5 forbids.
func (c *Chunker) Build(doc Document, src Source) ([]Chunk, error) {
	if err := ValidateDocument(doc, src); err != nil {
		return nil, err
	}
	norm := c.Norm
	if norm == nil {
		norm = NewNormalizer()
	}

	var (
		chunks []Chunk
		seen   = map[ChunkKey]int{}
	)
	for _, sec := range doc.Sections {
		if err := validateMappingRange(sec.MappingType, sec.AyahStart, sec.AyahEnd); err != nil {
			return nil, fmt.Errorf("section %d: %w", sec.Ordinal, err)
		}

		var pieces []string
		for _, para := range splitParagraphs(sec.Text) {
			pieces = append(pieces, splitLongParagraph(para, c.MaxRunes)...)
		}
		if len(pieces) == 0 {
			continue
		}

		// A section split across several paragraphs keeps one parent id so the
		// full verse-commentary section stays expandable (Part 1 §9).
		parent := ""
		if len(pieces) > 1 {
			parent = parentChunkID(src.ID, doc.SourceVersion, ChunkKey{
				AyahStart:   sec.AyahStart,
				AyahEnd:     sec.AyahEnd,
				MappingType: sec.MappingType,
				ContentHash: ContentHash(sec.Text),
			})
		}

		for _, piece := range pieces {
			hash := ContentHash(piece)
			key := ChunkKey{
				AyahStart:   sec.AyahStart,
				AyahEnd:     sec.AyahEnd,
				MappingType: sec.MappingType,
				ContentHash: hash,
			}
			dupIndex := seen[key]
			seen[key] = dupIndex + 1

			chunks = append(chunks, Chunk{
				ID:             ChunkID(src.ID, doc.SourceVersion, key, dupIndex),
				DocumentID:     doc.ID,
				ParentChunkID:  parent,
				SourceID:       src.ID,
				SourceVersion:  doc.SourceVersion,
				SurahID:        surahOf(sec, doc),
				AyahStart:      sec.AyahStart,
				AyahEnd:        sec.AyahEnd,
				MappingType:    sec.MappingType,
				Text:           piece,
				TextNormalized: norm.LexicalForm(piece),
				ContentHash:    hash,
				ContentType:    doc.ContentType,
				Language:       doc.Language,
				AuthorityTier:  src.AuthorityTier,
				SourceVerified: src.VerificationStatus == VerificationVerified && src.LicensingStatus == LicensingCleared,
				ReviewStatus:   src.VerificationStatus,
				CreatedAt:      timestamp(),
			})
		}
	}
	return chunks, nil
}

// surahOf prefers the section's own surah and falls back to the document's.
func surahOf(sec Section, doc Document) int {
	if sec.SurahID > 0 {
		return sec.SurahID
	}
	return doc.SurahID
}
