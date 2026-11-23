package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/answer"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

// Grade is the judge's verdict on one answer.
type Grade struct {
	Verdict string `json:"verdict"` // correct, partial, incorrect
	Reason  string `json:"reason"`
}

type AnswerResult struct {
	ID         string       `json:"id"`
	Question   string       `json:"question"`
	Reference  *string      `json:"reference"`
	Answerable bool         `json:"answerable"`
	Answer     string       `json:"answer"`
	Check      answer.Check `json:"check"`
	Citations  int          `json:"citations"`
	Grade      *Grade       `json:"grade,omitempty"` // answerable questions only
	Passed     bool         `json:"passed"`
	Usage      answer.Usage `json:"usage"`
	Error      string       `json:"error,omitempty"`
}

type AnswerReport struct {
	Date              string         `json:"date"`
	Model             string         `json:"model"`
	JudgeModel        string         `json:"judge_model"`
	Answerable        int            `json:"answerable"`
	Unanswerable      int            `json:"unanswerable"`
	Accuracy          float64        `json:"accuracy"`          // correct = 1, partial = 0.5
	FalseAbstentions  int            `json:"false_abstentions"` // said "not found" when it was there
	CorrectAbstains   int            `json:"correct_abstentions"`
	CitationPrecision float64        `json:"citation_precision"` // verified quotes / all quotes
	MeanGrounded      float64        `json:"mean_grounded"`
	RiskCounts        map[string]int `json:"risk_counts"`
	TotalTokens       int64          `json:"total_tokens"`
	Results           []AnswerResult `json:"results"`
}

const judgePrompt = `You grade answers from a question-answering system against a reference answer.

Verdicts:
- correct: the answer states the facts in the reference. Extra correct detail is fine.
- partial: some of the reference facts are there, or the answer is right but hedged as if unsure.
- incorrect: a reference fact is wrong or missing, or the answer says it couldn't find the information.

Judge facts, not wording. Reply with the verdict and a one-sentence reason.`

var gradeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"verdict": map[string]any{"type": "string", "enum": []string{"correct", "partial", "incorrect"}},
		"reason":  map[string]any{"type": "string"},
	},
	"required":             []string{"verdict", "reason"},
	"additionalProperties": false,
}

// Answers runs the full pipeline on every question and grades the results.
// It calls the model twice per answerable question (answer, then judge) and
// once per unanswerable one.
func Answers(ctx context.Context, r *retrieve.Retriever, a *answer.Answerer, judge answer.MessageClient, judgeModel string,
	qs []Question, opts retrieve.Options, outDir string, logf func(string, ...any)) (*AnswerReport, error) {
	rep := &AnswerReport{Date: time.Now().Format("2006-01-02"), Model: a.Cfg.Model, JudgeModel: judgeModel, RiskCounts: map[string]int{}}
	var verified, quotes int
	var grounded float64
	for _, q := range qs {
		res := AnswerResult{ID: q.ID, Question: q.Question, Reference: q.Answer, Answerable: q.Answerable}
		sources, err := r.Search(ctx, evalOwner, q.Question, opts)
		if err == nil {
			var ans *answer.Answer
			ans, err = a.Answer(ctx, q.Question, sources)
			if err == nil {
				res.Answer, res.Check, res.Usage = ans.Text, ans.Check, ans.Usage
				for _, s := range ans.Segments {
					for _, c := range s.Citations {
						res.Citations++
						quotes++
						if c.Verified {
							verified++
						}
					}
				}
			}
		}
		if err != nil {
			res.Error = err.Error()
		}

		if q.Answerable {
			rep.Answerable++
			if res.Error == "" {
				if res.Check.Abstained {
					rep.FalseAbstentions++
					res.Grade = &Grade{Verdict: "incorrect", Reason: "abstained although the documents contain the answer"}
				} else {
					g, usage, err := grade(ctx, judge, judgeModel, q, res.Answer)
					if err != nil {
						res.Error = "judge: " + err.Error()
					} else {
						res.Grade = g
						res.Usage.InputTokens += usage.InputTokens
						res.Usage.OutputTokens += usage.OutputTokens
					}
				}
			}
			if res.Grade != nil {
				switch res.Grade.Verdict {
				case "correct":
					rep.Accuracy++
					res.Passed = true
				case "partial":
					rep.Accuracy += 0.5
				}
			}
		} else {
			rep.Unanswerable++
			res.Passed = res.Error == "" && res.Check.Abstained
			if res.Passed {
				rep.CorrectAbstains++
			}
		}
		grounded += res.Check.Grounded
		rep.RiskCounts[res.Check.Risk]++
		rep.TotalTokens += res.Usage.Total()
		rep.Results = append(rep.Results, res)
		verdict := "abstain?"
		if res.Grade != nil {
			verdict = res.Grade.Verdict
		} else if !q.Answerable {
			verdict = map[bool]string{true: "abstained (ok)", false: "answered (wrong)"}[res.Passed]
		}
		logf("%s %-16s risk=%-6s %s", q.ID, verdict, res.Check.Risk, oneLine(q.Question, 60))
	}
	rep.Accuracy /= float64(max(rep.Answerable, 1))
	rep.MeanGrounded = grounded / float64(max(len(qs), 1))
	if quotes > 0 {
		rep.CitationPrecision = float64(verified) / float64(quotes)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	js, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "answers.json"), js, 0o644); err != nil {
		return nil, err
	}
	return rep, os.WriteFile(filepath.Join(outDir, "answers.md"), []byte(rep.Markdown()), 0o644)
}

func grade(ctx context.Context, client answer.MessageClient, model string, q Question, got string) (*Grade, answer.Usage, error) {
	prompt := fmt.Sprintf("Question: %s\n\nReference answer: %s\n\nAnswer to grade: %s", q.Question, deref(q.Answer), got)
	resp, err := client.New(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 2000,
		System:    []anthropic.BetaTextBlockParam{{Text: judgePrompt}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: gradeSchema},
		},
	})
	if err != nil {
		return nil, answer.Usage{}, err
	}
	u := answer.Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return nil, u, answer.ErrRefused
	}
	var text string
	for _, c := range resp.Content {
		if t, ok := c.AsAny().(anthropic.BetaTextBlock); ok {
			text += t.Text
		}
	}
	var g Grade
	if err := json.Unmarshal([]byte(text), &g); err != nil {
		return nil, u, fmt.Errorf("judge returned invalid JSON: %w", err)
	}
	return &g, u, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *AnswerReport) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Answer quality\n\nRun on %s with `%s` (judge: `%s`), hybrid search + rerank, top 6 chunks.\n\n", r.Date, r.Model, r.JudgeModel)
	sb.WriteString("| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&sb, "| Accuracy on %d answerable questions (partial = 0.5) | %s |\n", r.Answerable, pct(r.Accuracy))
	fmt.Fprintf(&sb, "| Said \"not found\" when the answer was there | %d |\n", r.FalseAbstentions)
	fmt.Fprintf(&sb, "| Correctly declined unanswerable questions | %d of %d |\n", r.CorrectAbstains, r.Unanswerable)
	fmt.Fprintf(&sb, "| Citation quotes found verbatim in their source | %s |\n", pct(r.CitationPrecision))
	fmt.Fprintf(&sb, "| Mean share of answer text with a citation | %s |\n", pct(r.MeanGrounded))
	fmt.Fprintf(&sb, "| Hallucination risk flags (low / medium / high) | %d / %d / %d |\n", r.RiskCounts["low"], r.RiskCounts["medium"], r.RiskCounts["high"])
	fmt.Fprintf(&sb, "| Tokens used (answers + grading) | %d |\n", r.TotalTokens)
	sb.WriteString("\n## Failures\n\n")
	n := 0
	for _, x := range r.Results {
		if x.Passed {
			continue
		}
		n++
		reason := x.Error
		if x.Grade != nil {
			reason = x.Grade.Verdict + ": " + x.Grade.Reason
		} else if !x.Answerable {
			reason = "answered a question the documents don't cover"
		}
		fmt.Fprintf(&sb, "- **%s** %s  \n  %s  \n  Answer: %s\n", x.ID, x.Question, reason, oneLine(x.Answer, 240))
	}
	if n == 0 {
		sb.WriteString("None.\n")
	}
	return sb.String()
}
