package answer

import (
	"regexp"
	"strings"

	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

var sentenceRe = regexp.MustCompile(`[^.!?\n]+[.!?]?`)

// Extractive answers without a language model: it returns the sentence (or
// table row) from the top sources that shares the most terms with the
// question, cited to its chunk. It is what the server does when no model is
// configured, and a baseline for the generated answers.
func Extractive(question string, sources []retrieve.Result, minConfidence float64) *Answer {
	q := map[string]bool{}
	for _, t := range embed.Tokens(question) {
		q[t] = true
	}
	bestScore, bestSrc, best := 0, -1, ""
	for i, s := range sources {
		if i >= 3 {
			break
		}
		body := s.Payload.Text
		if j := strings.Index(body, "\n"); j >= 0 && strings.Contains(body[:j], " > ") {
			body = body[j+1:] // skip the context header
		}
		var sents []string
		for _, sent := range sentenceRe.FindAllString(body, -1) {
			if sent = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(sent), "-*• ")); len(sent) >= 3 {
				sents = append(sents, sent)
			}
		}
		// Candidates are single sentences and pairs of neighbours, since a
		// fact is often split ("Snapshots run daily. Business: 30 days.").
		for j := range sents {
			for _, cand := range []string{sents[j], strings.Join(sents[j:min(j+2, len(sents))], " ")} {
				if len(cand) < 12 {
					continue
				}
				score := distinctHits(cand, q)
				// Higher overlap wins; on ties the shorter text and the
				// better-ranked source win.
				if score > bestScore || (score == bestScore && score > 0 && bestSrc == i && len(cand) < len(best)) {
					bestScore, bestSrc, best = score, i, cand
				}
			}
		}
	}

	ans := &Answer{Sources: sources}
	if bestSrc < 0 {
		msg := "I couldn't find this in your documents."
		ans.Text, ans.Segments = msg, []Segment{{Text: msg}}
	} else {
		s := sources[bestSrc]
		ans.Text = best
		ans.Segments = []Segment{{Text: best, Citations: []Citation{{
			Index: bestSrc, ChunkID: s.ID, DocName: s.Payload.DocName,
			Section: strings.Join(s.Payload.Section, " > "), CitedText: best, Verified: true,
		}}}}
	}
	ans.Usage.Model = "extractive"
	ans.Check = Validate(ans.Segments, sources, minConfidence)
	return ans
}

// distinctHits counts the distinct question terms that appear in s.
func distinctHits(s string, q map[string]bool) int {
	seen := map[string]bool{}
	for _, t := range embed.Tokens(s) {
		if q[t] {
			seen[t] = true
		}
	}
	return len(seen)
}
