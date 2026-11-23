package localllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/answer"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

// fakeOllama answers /api/chat with a fixed reply and records the request.
func fakeOllama(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"model": got["model"], "message": map[string]any{"content": reply}, "done_reason": "stop",
			"prompt_eval_count": 321, "eval_count": 45})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func sources() []retrieve.Result {
	mk := func(id, doc, text string) retrieve.Result {
		var r retrieve.Result
		r.ID = id
		r.Payload.DocName, r.Payload.Title, r.Payload.Text, r.Payload.Section = doc, doc, text, []string{doc}
		score := 0.9
		r.RerankScore = &score
		return r
	}
	return []retrieve.Result{
		mk("a", "pricing.md", "The Team plan costs $12 per user per month, billed annually."),
		mk("b", "limits.md", "Team workspaces can run up to 1,200 builds per month."),
	}
}

func TestAnswerWithEmulatedCitations(t *testing.T) {
	reply := `{"segments": [
	  {"text": "The Team plan costs $12 per user per month.", "citations": [{"source": 0, "quote": "costs $12 per user per month"}]},
	  {"text": "It allows 5,000 builds a month.", "citations": [{"source": 1, "quote": "up to 5,000 builds per month"}]}
	]}`
	srv, got := fakeOllama(t, reply)
	a := &answer.Answerer{LLM: &Client{BaseURL: srv.URL}, Cfg: answer.Config{Model: "ollama:qwen3:4b-instruct", MaxTokens: 16000, MinConfidence: 0.2}}
	ans, err := a.Answer(context.Background(), "How much is the Team plan and how many builds does it include?", sources())
	if err != nil {
		t.Fatal(err)
	}

	// The request: native model name, numbered sources, citation instructions, and a JSON constraint.
	req := *got
	if req["model"] != "qwen3:4b-instruct" {
		t.Errorf("model = %v", req["model"])
	}
	msgs := req["messages"].([]any)
	sys := msgs[0].(map[string]any)["content"].(string)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "Reply with JSON only") || !strings.Contains(sys, "couldn't find this in your documents") {
		t.Errorf("system prompt lacks the citation instructions or ragd's own rules:\n%s", sys)
	}
	if !strings.Contains(user, `<result index="1" title="limits.md">`) || !strings.HasSuffix(user, "builds does it include?\n</question>") {
		t.Errorf("user message:\n%s", user)
	}
	format, _ := req["format"].(map[string]any)
	segs, _ := format["properties"].(map[string]any)["segments"].(map[string]any)
	if segs["minItems"] != float64(1) {
		t.Errorf("segments must require at least one item, so a model can't answer with nothing: %v", segs)
	}
	if opts := req["options"].(map[string]any); opts["num_predict"].(float64) != 1500 {
		t.Errorf("num_predict = %v, want the 1500 cap", opts["num_predict"])
	}

	// The answer goes through ragd's normal checks.
	if len(ans.Segments) != 2 || ans.Text != "The Team plan costs $12 per user per month. It allows 5,000 builds a month." {
		t.Fatalf("segments %+v text %q", ans.Segments, ans.Text)
	}
	if c := ans.Segments[0].Citations; len(c) != 1 || !c[0].Verified || c[0].DocName != "pricing.md" {
		t.Errorf("first citation should verify against pricing.md: %+v", c)
	}
	if c := ans.Segments[1].Citations; len(c) != 1 || c[0].Verified {
		t.Errorf("the second quote is invented (5,000 vs 1,200), so it must not verify: %+v", c)
	}
	if ans.Check.UnverifiedQuotes != 1 || len(ans.Check.UnsourcedNumbers) == 0 {
		t.Errorf("check = %+v, want one unverified quote and the unsourced number flagged", ans.Check)
	}
	if ans.Usage.Model != "ollama:qwen3:4b-instruct" || ans.Usage.InputTokens != 321 || ans.Usage.OutputTokens != 45 {
		t.Errorf("usage = %+v", ans.Usage)
	}
}

func TestNonJSONReplyBecomesOneUncitedBlock(t *testing.T) {
	srv, _ := fakeOllama(t, `{"segments": [{"text": "The Team plan costs`)
	a := &answer.Answerer{LLM: &Client{BaseURL: srv.URL}, Cfg: answer.Config{Model: "ollama:m", MaxTokens: 100}}
	ans, err := a.Answer(context.Background(), "q?", sources())
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Segments) != 1 || len(ans.Segments[0].Citations) != 0 {
		t.Fatalf("segments = %+v", ans.Segments)
	}
}

func TestEmptySegmentsBecomeAnEmptyAnswer(t *testing.T) {
	srv, _ := fakeOllama(t, `{"segments": []}`)
	a := &answer.Answerer{LLM: &Client{BaseURL: srv.URL}, Cfg: answer.Config{Model: "ollama:m", MaxTokens: 100}}
	ans, err := a.Answer(context.Background(), "q?", sources())
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "" {
		t.Fatalf("text = %q, want empty (not the raw JSON)", ans.Text)
	}
}

func TestPlainCallPassesTheSchema(t *testing.T) {
	srv, got := fakeOllama(t, `{"correct": true, "reason": "matches"}`)
	schema := map[string]any{"type": "object", "properties": map[string]any{"correct": map[string]any{"type": "boolean"}}}
	resp, err := (&Client{BaseURL: srv.URL}).New(context.Background(), anthropic.BetaMessageNewParams{
		Model:     "ollama:llama3.2:3b",
		MaxTokens: 2000,
		System:    []anthropic.BetaTextBlockParam{{Text: "You grade answers."}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("Grade this."))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := *got
	if req["model"] != "llama3.2:3b" || req["format"].(map[string]any)["type"] != "object" {
		t.Errorf("request: %v", req)
	}
	tb, ok := resp.Content[0].AsAny().(anthropic.BetaTextBlock)
	if !ok || tb.Text != `{"correct": true, "reason": "matches"}` || resp.StopReason != anthropic.BetaStopReasonEndTurn {
		t.Errorf("response: %+v", resp)
	}
}
