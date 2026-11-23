// Package answer asks Claude to answer from retrieved chunks, then checks
// the answer: every citation must quote its source exactly, uncited claims
// are flagged, and numbers that appear in no source raise a warning.
package answer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

// MessageClient is the one Messages API call needed; tests replace it.
type MessageClient interface {
	New(ctx context.Context, params anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error)
}

type SDKClient struct{ Client anthropic.Client }

func (c SDKClient) New(ctx context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	return c.Client.Beta.Messages.New(ctx, p)
}

type Config struct {
	Model     string
	Effort    string
	MaxTokens int64
	Fallbacks bool
	// Below this retrieval confidence (0-1, from the reranker) the answer is
	// flagged: the knowledge base probably doesn't cover the question.
	MinConfidence float64
}

var DefaultConfig = Config{Model: "claude-opus-5-5", Effort: "medium", MaxTokens: 16000, Fallbacks: true, MinConfidence: 0.2}

const systemPrompt = `You answer questions using only the search results provided with the question. They come from the user's own documents.

Rules:
- Base every factual statement on the search results and cite them. Don't add facts from general knowledge.
- If the results don't contain the answer, say "I couldn't find this in your documents." and, if useful, mention what related information they do contain. Don't guess.
- If sources disagree (for example an old and a current value), give the current one and mention the change.
- Answer in a few sentences or a short list. Lead with the direct answer.`

// NotFound is the phrase the model is told to use when it abstains.
const NotFound = "couldn't find this in your documents"

type Citation struct {
	Index     int    `json:"index"`    // position of the source in Sources (0-based)
	ChunkID   string `json:"chunk_id"` // vector point id
	DocName   string `json:"doc_name"`
	Section   string `json:"section"`
	CitedText string `json:"cited_text"`
	Verified  bool   `json:"verified"` // the quote was found in the source chunk
}

// Segment is one piece of the answer with the citations that support it.
type Segment struct {
	Text      string     `json:"text"`
	Citations []Citation `json:"citations"`
}

type Check struct {
	Grounded          float64  `json:"grounded"`           // share of answer text that carries a citation
	UnsupportedClaims []string `json:"unsupported_claims"` // uncited segments that state something
	UnverifiedQuotes  int      `json:"unverified_quotes"`  // citations whose text isn't in the source
	UnsourcedNumbers  []string `json:"unsourced_numbers"`  // numbers in the answer found in no source
	Confidence        float64  `json:"retrieval_confidence"`
	Abstained         bool     `json:"abstained"`
	Risk              string   `json:"hallucination_risk"` // low, medium, high
	Warnings          []string `json:"warnings"`
}

type Usage struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

func (u Usage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

type Answer struct {
	Text     string            `json:"text"`
	Segments []Segment         `json:"segments"`
	Sources  []retrieve.Result `json:"sources"`
	Check    Check             `json:"check"`
	Usage    Usage             `json:"usage"`
}

type Answerer struct {
	LLM MessageClient
	Cfg Config
}

var ErrRefused = errors.New("the model declined to answer")

// Answer generates and checks an answer from already-retrieved sources.
func (a *Answerer) Answer(ctx context.Context, question string, sources []retrieve.Result) (*Answer, error) {
	if a.LLM == nil {
		return Extractive(question, sources, a.Cfg.MinConfidence), nil
	}
	content := make([]anthropic.BetaContentBlockParamUnion, 0, len(sources)+1)
	for _, s := range sources {
		block := anthropic.NewBetaSearchResultBlock(
			[]anthropic.BetaTextBlockParam{{Text: s.Payload.Text}},
			fmt.Sprintf("%s#%d", s.Payload.DocName, s.Payload.Index),
			title(s),
		)
		block.OfSearchResult.Citations = anthropic.BetaCitationsConfigParam{Enabled: anthropic.Bool(true)}
		content = append(content, block)
	}
	content = append(content, anthropic.NewBetaTextBlock(question))

	p := anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(a.Cfg.Model),
		MaxTokens:    a.Cfg.MaxTokens,
		System:       []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(content...)},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(a.Cfg.Effort)},
	}
	if a.Cfg.Fallbacks {
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}

	resp, err := a.LLM.New(ctx, p)
	if err != nil {
		return nil, err
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return nil, ErrRefused
	}

	ans := &Answer{Sources: sources, Usage: Usage{
		Model: string(resp.Model), InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
		CacheReadTokens: resp.Usage.CacheReadInputTokens, CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
	}}
	var sb strings.Builder
	for _, c := range resp.Content {
		tb, ok := c.AsAny().(anthropic.BetaTextBlock)
		if !ok {
			continue
		}
		seg := Segment{Text: tb.Text}
		for _, cit := range tb.Citations {
			if loc, ok := cit.AsAny().(anthropic.BetaCitationSearchResultLocation); ok {
				seg.Citations = append(seg.Citations, citation(loc, sources))
			}
		}
		ans.Segments = append(ans.Segments, seg)
		sb.WriteString(tb.Text)
	}
	ans.Text = strings.TrimSpace(sb.String())
	ans.Check = Validate(ans.Segments, sources, a.Cfg.MinConfidence)
	return ans, nil
}

func title(s retrieve.Result) string {
	t := s.Payload.Title
	if len(s.Payload.Section) > 1 {
		t += " > " + strings.Join(s.Payload.Section[1:], " > ")
	}
	return t
}

func citation(loc anthropic.BetaCitationSearchResultLocation, sources []retrieve.Result) Citation {
	c := Citation{Index: int(loc.SearchResultIndex), CitedText: loc.CitedText}
	if c.Index >= 0 && c.Index < len(sources) {
		s := sources[c.Index]
		c.ChunkID, c.DocName = s.ID, s.Payload.DocName
		c.Section = strings.Join(s.Payload.Section, " > ")
		c.Verified = containsNormalized(s.Payload.Text, loc.CitedText)
	}
	return c
}

// ---- checks ----

var (
	numberRe = regexp.MustCompile(`\d[\d,.]*\d|\d`)
	letterRe = regexp.MustCompile(`[A-Za-z]`)
	wordsRe  = regexp.MustCompile(`\S+`)
	spacesRe = regexp.MustCompile(`\s+`)
)

// Validate scores how well the answer is supported by its sources. It is
// deterministic, so it runs on every answer, not only in evaluation.
func Validate(segs []Segment, sources []retrieve.Result, minConfidence float64) Check {
	c := Check{Confidence: retrieve.Confidence(sources)}
	full := ""
	for _, s := range segs {
		full += s.Text
	}
	c.Abstained = strings.Contains(strings.ToLower(strings.ReplaceAll(full, "’", "'")), NotFound)

	var total, cited int
	for _, s := range segs {
		n := len(strings.TrimSpace(s.Text))
		if n == 0 {
			continue
		}
		total += n
		if len(s.Citations) > 0 {
			cited += n
			for _, ci := range s.Citations {
				if !ci.Verified {
					c.UnverifiedQuotes++
				}
			}
			continue
		}
		// Uncited connective text ("Yes," or "and") is fine; uncited
		// statements with substance are not.
		if isClaim(s.Text) {
			c.UnsupportedClaims = append(c.UnsupportedClaims, strings.TrimSpace(s.Text))
		}
	}
	if total > 0 {
		c.Grounded = math.Round(float64(cited)/float64(total)*100) / 100
	}

	var corpus strings.Builder
	for _, s := range sources {
		corpus.WriteString(s.Payload.Text)
		corpus.WriteByte('\n')
	}
	src := normalizeNumbers(corpus.String())
	seen := map[string]bool{}
	for _, n := range numberRe.FindAllString(full, -1) {
		n = strings.TrimRight(n, ".,")
		if seen[n] || len(n) == 0 {
			continue
		}
		seen[n] = true
		if !strings.Contains(src, normalizeNumbers(n)) {
			c.UnsourcedNumbers = append(c.UnsourcedNumbers, n)
		}
	}

	switch {
	case c.Abstained:
		c.Risk = "low"
	case c.UnverifiedQuotes > 0 || len(c.UnsourcedNumbers) > 0:
		c.Risk = "high"
	case c.Grounded < 0.5 || len(c.UnsupportedClaims) > 0 || (c.Confidence >= 0 && c.Confidence < minConfidence):
		c.Risk = "medium"
	default:
		c.Risk = "low"
	}
	if c.UnverifiedQuotes > 0 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("%d citation(s) quote text that is not in the cited source", c.UnverifiedQuotes))
	}
	if len(c.UnsourcedNumbers) > 0 {
		c.Warnings = append(c.Warnings, "numbers not found in any source: "+strings.Join(c.UnsourcedNumbers, ", "))
	}
	if len(c.UnsupportedClaims) > 0 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("%d statement(s) have no citation", len(c.UnsupportedClaims)))
	}
	if !c.Abstained && c.Confidence >= 0 && c.Confidence < minConfidence {
		c.Warnings = append(c.Warnings, fmt.Sprintf("low retrieval confidence (%.2f): your documents may not cover this", c.Confidence))
	}
	return c
}

func isClaim(s string) bool {
	s = strings.TrimSpace(s)
	return letterRe.MatchString(s) && (len(wordsRe.FindAllString(s, -1)) >= 6 || numberRe.MatchString(s))
}

func containsNormalized(haystack, needle string) bool {
	n := strings.TrimSpace(spacesRe.ReplaceAllString(needle, " "))
	if n == "" {
		return false
	}
	return strings.Contains(spacesRe.ReplaceAllString(haystack, " "), n)
}

var thousandsRe = regexp.MustCompile(`(\d),(\d{3})`)

// normalizeNumbers drops thousands separators so "1,200" matches "1200".
// It loops because matches can't overlap ("1,200,000" needs two passes).
func normalizeNumbers(s string) string {
	for {
		next := thousandsRe.ReplaceAllString(s, "$1$2")
		if next == s {
			return s
		}
		s = next
	}
}
