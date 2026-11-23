// Package localllm answers ragd's Messages API calls with a local model
// served by Ollama, so answers and the answer evaluation can run without an
// API key.
//
// Ollama has an Anthropic-compatible endpoint, but it rejects search_result
// blocks and ignores structured-output schemas, and ragd needs both. This
// adapter emulates them on Ollama's native chat API instead:
//
//   - Search results become numbered sources in the prompt. The model
//     answers in JSON segments, each with the sources it used and a quote
//     copied from each, and the adapter turns those into Claude-style
//     search_result_location citations. ragd's checks then verify every
//     quote against its chunk exactly as they do for Claude.
//   - An output_config.format schema becomes Ollama's format, which
//     constrains decoding to that JSON shape.
//
// Model names are "ollama:<name>", e.g. "ollama:qwen3:4b-instruct".
package localllm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const Prefix = "ollama:"

type Client struct {
	BaseURL   string // default http://127.0.0.1:11434
	MaxTokens int64  // cap on output tokens per call; default 1500
	HTTP      *http.Client
}

func (c *Client) base() string {
	if c.BaseURL == "" {
		return "http://127.0.0.1:11434"
	}
	b := c.BaseURL
	if !strings.HasPrefix(b, "http") {
		b = "http://" + b
	}
	return strings.TrimRight(b, "/")
}

type source struct{ title, name, text string }

// The answer shape a local model fills in when search results are present.
var segmentsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"segments": map[string]any{
			"type": "array",
			// At least one segment, so a model with nothing to cite has to
			// write the not-found sentence instead of returning nothing.
			"minItems": 1,
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
					"citations": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"source": map[string]any{"type": "integer"},
								"quote":  map[string]any{"type": "string"},
							},
							"required": []string{"source", "quote"},
						},
					},
				},
				"required": []string{"text", "citations"},
			},
		},
	},
	"required": []string{"segments"},
}

const citeInstructions = `

How to reply: the search results are numbered from 0. Reply with JSON only, in this shape:
{"segments": [{"text": "one sentence of your answer", "citations": [{"source": 0, "quote": "a few words copied exactly from search result 0"}]}]}
- Split the answer into sentences. Give each factual sentence the search result(s) it comes from, each with a short quote copied word for word from that result.
- A sentence that states no fact has no citations.
- If the search results don't contain the answer, reply with exactly one segment: {"text": "I couldn't find this in your documents.", "citations": []}`

type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResp struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int64  `json:"prompt_eval_count"`
	EvalCount       int64  `json:"eval_count"`
	Error           string `json:"error"`
}

// New implements answer.MessageClient (and the eval's judge client).
func (c *Client) New(ctx context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	model := strings.TrimPrefix(string(p.Model), Prefix)
	var system []string
	for _, s := range p.System {
		system = append(system, s.Text)
	}
	var sources []source
	var msgs []ollamaMsg
	for _, m := range p.Messages {
		role, blocks := "user", []anthropic.BetaContentBlockParamUnion(nil)
		switch {
		case m.OfUser != nil:
			blocks = m.OfUser.Content
		case m.OfAssistant != nil:
			role, blocks = "assistant", m.OfAssistant.Content
		default:
			continue
		}
		var text []string
		for _, b := range blocks {
			switch {
			case b.OfText != nil:
				text = append(text, b.OfText.Text)
			case b.OfSearchResult != nil:
				var parts []string
				for _, t := range b.OfSearchResult.Content {
					parts = append(parts, t.Text)
				}
				sources = append(sources, source{title: b.OfSearchResult.Title, name: b.OfSearchResult.Source, text: strings.Join(parts, "\n")})
			}
		}
		msgs = append(msgs, ollamaMsg{Role: role, Content: strings.Join(text, "\n\n")})
	}

	var format any
	if len(sources) > 0 {
		system = append(system, citeInstructions)
		var sb strings.Builder
		sb.WriteString("<search_results>\n")
		for i, s := range sources {
			fmt.Fprintf(&sb, "<result index=\"%d\" title=%q>\n%s\n</result>\n", i, s.title, s.text)
		}
		sb.WriteString("</search_results>\n\n")
		// The search results belong to the last user turn, ahead of its question.
		last := len(msgs) - 1
		msgs[last].Content = sb.String() + "<question>\n" + msgs[last].Content + "\n</question>"
		format = segmentsSchema
	} else if p.OutputConfig.Format.Schema != nil {
		format = p.OutputConfig.Format.Schema
	}
	if len(system) > 0 {
		msgs = append([]ollamaMsg{{Role: "system", Content: strings.Join(system, "\n\n")}}, msgs...)
	}

	maxTok := p.MaxTokens
	if limit := c.MaxTokens; limit == 0 {
		maxTok = min(maxTok, 1500)
	} else {
		maxTok = min(maxTok, limit)
	}
	body := map[string]any{
		"model": model, "stream": false, "messages": msgs,
		"options": map[string]any{"temperature": 0, "seed": 42, "num_predict": maxTok},
	}
	if format != nil {
		body["format"] = format
	}
	out, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(out.Message.Content)
	if i := strings.LastIndex(content, "</think>"); i >= 0 {
		content = strings.TrimSpace(content[i+len("</think>"):])
	}
	stop := "end_turn"
	if out.DoneReason == "length" {
		stop = "max_tokens"
	}

	var blocks []map[string]any
	if len(sources) > 0 {
		blocks = citedBlocks(content, sources)
	} else {
		blocks = []map[string]any{{"type": "text", "text": content}}
	}
	msg := map[string]any{
		"id": fmt.Sprintf("msg_local_%d", time.Now().UnixNano()), "type": "message", "role": "assistant",
		"model": Prefix + model, "content": blocks, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": out.PromptEvalCount, "output_tokens": out.EvalCount},
	}
	raw, _ := json.Marshal(msg)
	var bm anthropic.BetaMessage
	if err := json.Unmarshal(raw, &bm); err != nil {
		return nil, fmt.Errorf("localllm: building the response: %w", err)
	}
	return &bm, nil
}

// citedBlocks turns the model's JSON segments into text blocks with
// search_result_location citations. Quotes are passed through as written,
// so a misquote fails ragd's quote check instead of being fixed here. If
// the reply isn't the expected JSON (a cut-off answer), it is returned as
// one uncited block, which the checks then score as unsupported.
func citedBlocks(content string, sources []source) []map[string]any {
	var reply struct {
		Segments []struct {
			Text      string `json:"text"`
			Citations []struct {
				Source int    `json:"source"`
				Quote  string `json:"quote"`
			} `json:"citations"`
		} `json:"segments"`
	}
	if err := json.Unmarshal([]byte(content), &reply); err != nil {
		return []map[string]any{{"type": "text", "text": content}}
	}
	if len(reply.Segments) == 0 {
		return []map[string]any{{"type": "text", "text": ""}} // valid JSON with nothing in it: an empty answer
	}
	var blocks []map[string]any
	for i, seg := range reply.Segments {
		text := strings.TrimSpace(seg.Text)
		if i < len(reply.Segments)-1 {
			text += " "
		}
		var cits []map[string]any
		for _, cit := range seg.Citations {
			c := map[string]any{"type": "search_result_location", "cited_text": cit.Quote, "search_result_index": cit.Source,
				"start_block_index": 0, "end_block_index": 0, "source": "", "title": ""}
			if cit.Source >= 0 && cit.Source < len(sources) {
				c["source"], c["title"] = sources[cit.Source].name, sources[cit.Source].title
			}
			cits = append(cits, c)
		}
		b := map[string]any{"type": "text", "text": text}
		if len(cits) > 0 {
			b["citations"] = cits
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func (c *Client) post(ctx context.Context, body map[string]any) (*ollamaResp, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("localllm: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var out ollamaResp
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		msg := out.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("localllm: HTTP %d: %s", resp.StatusCode, msg)
	}
	return &out, nil
}
