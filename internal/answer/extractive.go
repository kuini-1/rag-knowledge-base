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
		// Sentence or line boundaries, as byte ranges, so every candidate is
		// an exact span of the source (the quote must match it verbatim).
		var spans [][2]int
		for _, m := range sentenceRe.FindAllStringIndex(body, -1) {
			start, end := m[0], m[1]
			for start < end && strings.ContainsRune(" \t-*•", rune(body[start])) {
				start++
			}
			for end > start && (body[end-1] == ' ' || body[end-1] == '\t') {
				end--
			}
			if end-start >= 3 {
				spans = append(spans, [2]int{start, end})
			}
		}
		// Candidates are windows of one to three neighbouring sentences or
		// lines, since a fact is often split across them ("Retention depends
		// on the plan:" followed by "Business: 30 days").
		for j := range spans {
			for k := 1; k <= 3 && j+k <= len(spans); k++ {
				cand := body[spans[j][0]:spans[j+k-1][1]]
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
