package review

// ReviewRecord mirrors the "reviews" collection — see
// docs/rag-architecture-part2.md §23-24.
type ReviewRecord struct {
	ID            string `bson:"_id,omitempty" json:"id"`
	ExplanationID string `bson:"explanation_id" json:"explanation_id"`
	ReviewerID    string `bson:"reviewer_id" json:"reviewer_id"`
	Decision      string `bson:"decision" json:"decision"` // approve | edit_approve | reject | escalate
	Comments      string `bson:"comments,omitempty" json:"comments,omitempty"`
	CreatedAt     string `bson:"created_at" json:"created_at"`

	// SourceVersion and PromptVersion are the grounding in effect at review time.
	// Part 1 §20 requires them on the record so a later source change can be
	// traced back to the exact text a reviewer actually approved, instead of to
	// whatever the source says now. PromptVersion is empty until the generation
	// batch (issue #10) starts stamping it on the explanations it writes.
	SourceVersion string `bson:"source_version,omitempty" json:"source_version,omitempty"`
	PromptVersion string `bson:"prompt_version,omitempty" json:"prompt_version,omitempty"`

	// ModelVersion names the LLM that produced the text under review, so a bad
	// generation run can be re-run rather than re-litigated verse by verse.
	ModelVersion string `bson:"model_version,omitempty" json:"model_version,omitempty"`
}

// QueueItem is one pending explanation as the dashboard needs it: the text, the
// verse it claims to explain, and the chunks it was grounded in.
//
// It deliberately restates the fields content.Explanation carries rather than
// embedding it. content.Explanation is the reader-facing projection and hides
// review_status and source_version_snapshot from the API on purpose — the
// reviewer needs both, and relaxing that projection to serve an internal tool
// would widen what the public reader can see.
type QueueItem struct {
	ID         string   `bson:"_id" json:"id"`
	SurahID    int      `bson:"surah_id" json:"surah_id"`
	AyahStart  int      `bson:"ayah_start" json:"ayah_start"`
	AyahEnd    int      `bson:"ayah_end" json:"ayah_end"`
	Style      string   `bson:"style" json:"style"`
	Level      string   `bson:"level" json:"level"`
	Text       string   `bson:"text" json:"text"`
	SourceRefs []string `bson:"source_refs" json:"source_refs"`

	ReviewStatus          string `bson:"review_status" json:"review_status"`
	SourceVersionSnapshot string `bson:"source_version_snapshot" json:"source_version_snapshot"`
	PromptVersion         string `bson:"prompt_version,omitempty" json:"prompt_version,omitempty"`
	ModelVersion          string `bson:"model_version,omitempty" json:"model_version,omitempty"`

	// AyahText is the Quranic text being explained, from the verified corpus —
	// never from the explanation itself. A reviewer checking "no added or
	// removed meaning" needs both sides side by side.
	AyahText string `json:"ayah_text"`

	// Evidence is the resolved source_refs, in the order the explanation cites
	// them. MissingRefs holds refs that no longer resolve to a chunk; a
	// non-empty MissingRefs means the explanation is grounded in something the
	// reviewer cannot read, and must not be approved.
	Evidence    []Evidence `json:"evidence"`
	MissingRefs []string   `json:"missing_refs"`

	// Citation is the source registry entry for the evidence's source, which is
	// what makes a citation checkable rather than decorative (Part 1 §15).
	Citation Citation `json:"citation"`

	// GroundingVersionMismatch is true when the explanation's
	// source_version_snapshot disagrees with the version its evidence chunks were
	// actually built from — i.e. the source changed after this text was
	// generated. Part 1 §20 requires such an explanation to be re-reviewed rather
	// than published on the strength of an approval given against different bytes,
	// so the dashboard blocks on it.
	GroundingVersionMismatch bool `json:"grounding_version_mismatch"`
}

// Evidence is one resolved tafsir chunk behind an explanation's claim.
type Evidence struct {
	ChunkID       string `bson:"_id" json:"chunk_id"`
	Text          string `bson:"text" json:"text"`
	AyahStart     int    `bson:"ayah_start" json:"ayah_start"`
	AyahEnd       int    `bson:"ayah_end" json:"ayah_end"`
	MappingType   string `bson:"mapping_type" json:"mapping_type"`
	SourceID      string `bson:"source_id" json:"source_id"`
	SourceVersion string `bson:"source_version" json:"source_version"`
	ContentHash   string `bson:"content_hash" json:"content_hash"`
}

// Citation identifies the source a published explanation draws on.
type Citation struct {
	SourceID string `json:"source_id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Edition  string `json:"edition,omitempty"`
	Version  string `json:"version,omitempty"`
}
