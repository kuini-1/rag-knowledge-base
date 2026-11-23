package answer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

func src(doc string, idx int, text string, rerank float64) retrieve.Result {
	return retrieve.Result{
		Hit:         vector.Hit{ID: doc, Payload: vector.Payload{DocName: doc, Title: doc, Index: idx, Text: text, Section: []string{doc, "Limits"}}},
		RerankScore: &rerank,
	}
}

var sources = []retrieve.Result{
	src("api-rate-limits.md", 1, "| Plan | Requests per minute |\n| Team | 1,200 | 200 | 16 |", 3),
	src("changelog.md", 4, "Raised the Team plan rate limit from 600 to 1,200 requests per minute.", 1),
}

type fake struct {
	reply string
	got   anthropic.BetaMessageNewParams
}

func (f *fake) New(_ context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	f.got = p
	var m anthropic.BetaMessage
	return &m, json.Unmarshal([]byte(f.reply), &m)
}

func reply(blocks ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"stop_reason": "end_turn", "content": blocks,
		"usage": map[string]any{"input_tokens": 900, "output_tokens": 60},
	})
	return string(b)
}

func text(s string, cites ...map[string]any) map[string]any {
	m := map[string]any{"type": "text", "text": s}
	if len(cites) > 0 {
		m["citations"] = cites
	}
	return m
}

func cite(i int, quote string) map[string]any {
	return map[string]any{"type": "search_result_location", "search_result_index": i, "cited_text": quote,
		"source": "x", "title": "x", "start_block_index": 0, "end_block_index": 1}
}

func TestAnswerWithVerifiedCitations(t *testing.T) {
	f := &fake{reply: reply(
		text("The Team plan allows "),
		text("1,200 requests per minute", cite(0, "| Team | 1,200 | 200 | 16 |")),
		text(", up from 600.", cite(1, "Raised the Team plan rate limit from 600 to 1,200 requests per minute.")),
	)}
	a := &Answerer{LLM: f, Cfg: DefaultConfig}
	ans, err := a.Answer(context.Background(), "What is the Team rate limit?", sources)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Check.Risk != "low" || len(ans.Check.Warnings) != 0 {
		t.Errorf("risk = %s, warnings = %v", ans.Check.Risk, ans.Check.Warnings)
	}
	if ans.Check.UnverifiedQuotes != 0 {
		t.Error("citations should verify against their sources")
	}
	if ans.Segments[1].Citations[0].DocName != "api-rate-limits.md" {
		t.Errorf("citation mapped to %q", ans.Segments[1].Citations[0].DocName)
	}
	if ans.Usage.Total() != 960 {
		t.Errorf("usage = %d", ans.Usage.Total())
	}

	// Every source is sent as a search_result block with citations on, before the question.
	b, _ := json.Marshal(f.got.Messages[0])
	if strings.Count(string(b), `"type":"search_result"`) != 2 || !strings.Contains(string(b), `"citations":{"enabled":true}`) {
		t.Errorf("request did not carry search results with citations: %s", b)
	}
}

func TestFabricatedQuoteAndNumberAreFlagged(t *testing.T) {
	segs := []Segment{
		{Text: "The Team plan allows 5,000 requests per minute.", Citations: []Citation{{Index: 0, CitedText: "Team | 5,000", Verified: false}}},
	}
	c := Validate(segs, sources, 0.2)
	if c.Risk != "high" {
		t.Errorf("risk = %s, want high", c.Risk)
	}
	if c.UnverifiedQuotes != 1 || len(c.UnsourcedNumbers) != 1 || c.UnsourcedNumbers[0] != "5,000" {
		t.Errorf("check = %+v", c)
	}
}

func TestUncitedClaimIsMediumRisk(t *testing.T) {
	segs := []Segment{{Text: "Business customers can also request custom limits from their account manager."}}
	c := Validate(segs, sources, 0.2)
	if c.Risk != "medium" || len(c.UnsupportedClaims) != 1 {
		t.Errorf("check = %+v", c)
	}
}

func TestAbstentionIsLowRisk(t *testing.T) {
	low := -4.0
	weak := []retrieve.Result{sources[0]}
	weak[0].RerankScore = &low
	segs := []Segment{{Text: "I couldn't find this in your documents."}}
	c := Validate(segs, weak, 0.2)
	if !c.Abstained || c.Risk != "low" {
		t.Errorf("check = %+v", c)
	}
}

func TestLowConfidenceWarning(t *testing.T) {
	low := -4.0
	weak := []retrieve.Result{sources[0]}
	weak[0].RerankScore = &low
	segs := []Segment{{Text: "1,200", Citations: []Citation{{Verified: true}}}}
	c := Validate(segs, weak, 0.2)
	if c.Risk != "medium" || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "low retrieval confidence") {
		t.Errorf("check = %+v", c)
	}
}

func TestNormalizeNumbers(t *testing.T) {
	if got := normalizeNumbers("1,200,000 and 5,000"); got != "1200000 and 5000" {
		t.Errorf("got %q", got)
	}
}
