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
}
