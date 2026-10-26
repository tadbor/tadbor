package introduction

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"tadbor/backend/internal/content"
	"tadbor/backend/internal/generation"
)

const PilotSurahID = 12

var (
	ErrInsufficientEvidence = errors.New("insufficient verified introduction evidence")
	ErrInvalidDraft         = errors.New("invalid introduction draft")
)

type EvidenceSource interface {
	LoadEvidence(context.Context, int) ([]generation.Evidence, error)
}

type Generator interface {
	GenerateIntroduction(context.Context, generation.Request) (*generation.GenerationOutput, error)
}

type DraftStore interface {
	SavePending(context.Context, content.SurahIntroduction) error
}

type Pipeline struct {
	evidence  EvidenceSource
	generator Generator
	store     DraftStore
	now       func() time.Time
}

func NewPipeline(evidence EvidenceSource, generator Generator, store DraftStore) *Pipeline {
	return &Pipeline{evidence: evidence, generator: generator, store: store, now: time.Now}
}

func (p *Pipeline) Run(ctx context.Context, surahID int, style, level string) (*content.SurahIntroduction, error) {
	if surahID != PilotSurahID {
		return nil, fmt.Errorf("introduction pilot supports surah %d only", PilotSurahID)
	}
	if strings.TrimSpace(style) == "" || strings.TrimSpace(level) == "" {
		return nil, fmt.Errorf("style and level are required")
	}
	evidence, err := p.evidence.LoadEvidence(ctx, surahID)
	if err != nil {
		return nil, fmt.Errorf("load verified evidence: %w", err)
	}
	if len(evidence) == 0 {
		return nil, ErrInsufficientEvidence
	}
	if err := validateEvidence(evidence); err != nil {
		return nil, err
	}

	language, err := targetLanguage(style)
	if err != nil {
		return nil, err
	}
	request := generation.Request{
		Task: "introduce", TargetLanguage: language, TargetStyle: style, Evidence: evidence,
	}
	output, err := p.generator.GenerateIntroduction(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("generate introduction: %w", err)
	}
	if err := Validate(output, evidence, style); err != nil {
		return nil, err
	}

	sections, refs, err := resolveSections(output.Sections, evidence)
	if err != nil {
		return nil, err
	}
	differing, differingRefs, err := resolveClaims(output.DifferingReports, evidence)
	if err != nil {
		return nil, err
	}
	refs = unique(append(refs, differingRefs...))
	evidenceByID := make(map[string]generation.Evidence, len(evidence))
	for _, item := range evidence {
		evidenceByID[item.ID] = item
	}
	versions := make([]string, 0, len(refs))
	for _, ref := range refs {
		item, ok := evidenceByID[ref]
		if !ok {
			return nil, fmt.Errorf("%w: cited source_ref %q does not resolve", ErrInvalidDraft, ref)
		}
		versions = append(versions, item.SourceVersion)
	}
	sort.Strings(versions)
	versions = unique(versions)

	draft := content.SurahIntroduction{
		ID:      fmt.Sprintf("surah-%d-%s", surahID, style),
		SurahID: surahID, Style: style, Level: level, Sections: sections,
		SourceRefs: refs, DifferingReports: differing, ReviewStatus: "pending",
		SourceVersionSnapshot: strings.Join(versions, "+"),
		PromptVersion:         generation.PromptVersion(), ModelVersion: generation.ModelVersion(),
		CreatedAt: p.now().UTC(), UpdatedAt: p.now().UTC(),
	}
	if err := p.store.SavePending(ctx, draft); err != nil {
		return nil, fmt.Errorf("persist introduction draft: %w", err)
	}
	return &draft, nil
}

func Validate(output *generation.GenerationOutput, evidence []generation.Evidence, style string) error {
	if output == nil || len(output.Sections) == 0 {
		return fmt.Errorf("%w: no typed sections returned", ErrInvalidDraft)
	}
	if !output.RequiresReview {
		return fmt.Errorf("%w: generated introduction must require human review", ErrInvalidDraft)
	}
	if err := validateEvidence(evidence); err != nil {
		return err
	}
	known := make(map[string]bool, len(evidence))
	for _, item := range evidence {
		known[item.ID] = true
	}
	validTypes := map[string]bool{"classification": true, "context": true, "themes": true, "stories": true, "asbab": true}
	for sectionIndex, section := range output.Sections {
		if !validTypes[section.Type] || strings.TrimSpace(section.Title) == "" || len(section.Claims) == 0 {
			return fmt.Errorf("%w: section %d needs an allowed type, title, and claims", ErrInvalidDraft, sectionIndex)
		}
		for claimIndex, claim := range section.Claims {
			if err := validateClaim(claim, known, len(evidence)); err != nil {
				return fmt.Errorf("section %d claim %d: %w", sectionIndex, claimIndex, err)
			}
		}
	}
	for claimIndex, claim := range output.DifferingReports {
		if err := validateClaim(claim, known, len(evidence)); err != nil {
			return fmt.Errorf("differing report %d: %w", claimIndex, err)
		}
	}
	var text []string
	for _, section := range output.Sections {
		for _, claim := range section.Claims {
			text = append(text, claim.Text)
		}
	}
	for _, claim := range output.DifferingReports {
		text = append(text, claim.Text)
	}
	if err := generation.ValidateTargetScript(style, strings.Join(text, " ")); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDraft, err)
	}
	return nil
}

func targetLanguage(style string) (string, error) {
	switch style {
	case "simplified_ar", "egyptian_ar":
		return "ar", nil
	case "en":
		return "en", nil
	default:
		return "", fmt.Errorf("unsupported introduction style %q", style)
	}
}

func validateEvidence(evidence []generation.Evidence) error {
	seen := map[string]bool{}
	for _, item := range evidence {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" ||
			strings.TrimSpace(item.SourceID) == "" || strings.TrimSpace(item.SourceVersion) == "" {
			return fmt.Errorf("%w: each evidence item needs an id, text, verified source id, and source version", ErrInsufficientEvidence)
		}
		if seen[item.ID] {
			return fmt.Errorf("%w: duplicate evidence id %q", ErrInsufficientEvidence, item.ID)
		}
		seen[item.ID] = true
	}
	return nil
}

func validateClaim(claim generation.IntroductionClaim, known map[string]bool, evidenceCount int) error {
	if strings.TrimSpace(claim.Text) == "" {
		return fmt.Errorf("%w: claim text is empty", ErrInvalidDraft)
	}
	if len(claim.SourceRefs) == 0 {
		return fmt.Errorf("%w: claim has no source_refs", ErrInvalidDraft)
	}
	seen := map[string]bool{}
	for _, ref := range claim.SourceRefs {
		if !known[ref] && !validPositionRef(ref, evidenceCount) {
			return fmt.Errorf("%w: source_ref %q does not resolve to supplied verified evidence", ErrInvalidDraft, ref)
		}
		if seen[ref] {
			return fmt.Errorf("%w: duplicate source_ref %q", ErrInvalidDraft, ref)
		}
		seen[ref] = true
	}
	return nil
}

func validPositionRef(ref string, evidenceCount int) bool {
	position, err := strconv.Atoi(strings.TrimSpace(ref))
	return err == nil && position > 0 && position <= evidenceCount
}

func resolveSections(input []generation.IntroductionSection, evidence []generation.Evidence) ([]content.Section, []string, error) {
	sections := make([]content.Section, 0, len(input))
	var refs []string
	for _, section := range input {
		claims, sectionRefs, err := resolveClaims(section.Claims, evidence)
		if err != nil {
			return nil, nil, err
		}
		sections = append(sections, content.Section{
			Type: section.Type, Title: section.Title, Content: strings.Join(claimTexts(claims), " "),
			SourceRefs: sectionRefs, Claims: claims,
		})
		refs = append(refs, sectionRefs...)
	}
	return sections, unique(refs), nil
}

func resolveClaims(input []generation.IntroductionClaim, evidence []generation.Evidence) ([]content.Claim, []string, error) {
	byID := make(map[string]bool, len(evidence))
	for _, item := range evidence {
		byID[item.ID] = true
	}
	claims := make([]content.Claim, 0, len(input))
	var refs []string
	for _, claim := range input {
		resolved := make([]string, 0, len(claim.SourceRefs))
		for _, ref := range claim.SourceRefs {
			if n, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil {
				if n < 1 || n > len(evidence) {
					return nil, nil, fmt.Errorf("%w: model cited evidence [%s], which was not provided", ErrInvalidDraft, ref)
				}
				ref = evidence[n-1].ID
			}
			if !byID[ref] {
				return nil, nil, fmt.Errorf("%w: source_ref %q does not resolve", ErrInvalidDraft, ref)
			}
			resolved = append(resolved, ref)
			refs = append(refs, ref)
		}
		claims = append(claims, content.Claim{Text: claim.Text, SourceRefs: unique(resolved)})
	}
	return claims, unique(refs), nil
}

func claimTexts(claims []content.Claim) []string {
	texts := make([]string, 0, len(claims))
	for _, claim := range claims {
		texts = append(texts, claim.Text)
	}
	return texts
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
