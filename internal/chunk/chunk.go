// Package chunk splits a parsed document into pieces for embedding.
//
// Two strategies are compared in the evaluation:
//
//   - fixed: a sliding window over the raw text, ignoring structure.
//   - structure: packs whole blocks (paragraphs, lists, tables) within one
//     section, splits oversized blocks on sentence or row boundaries, and
//     repeats a table's header row in every piece of the table.
//
// With ContextHeader on, each chunk's embedded text starts with the document
// title and section path ("SLA > Uptime commitments > Business"), so a
// passage that only says "this plan" still carries what it is about.
package chunk

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kuini-1/rag-knowledge-base/internal/parse"
)

type Config struct {
	Strategy      string `json:"strategy"`       // "fixed" or "structure"
	Size          int    `json:"size"`           // target size in estimated tokens
	Overlap       int    `json:"overlap"`        // tokens repeated from the previous chunk
	ContextHeader bool   `json:"context_header"` // prefix title and section path
}

func (c Config) Name() string {
	h := ""
	if c.ContextHeader {
		h = "-ctx"
	}
	return fmt.Sprintf("%s-%d-%d%s", c.Strategy, c.Size, c.Overlap, h)
}

func (c Config) Validate() error {
	if c.Strategy != "fixed" && c.Strategy != "structure" {
		return fmt.Errorf("unknown strategy %q", c.Strategy)
	}
	if c.Size < 32 || c.Size > 4096 {
		return fmt.Errorf("size %d out of range (32-4096)", c.Size)
	}
	if c.Overlap < 0 || c.Overlap >= c.Size/2 {
		return fmt.Errorf("overlap must be between 0 and half the size")
	}
	return nil
}

var Default = Config{Strategy: "structure", Size: 256, Overlap: 32, ContextHeader: true}

type Chunk struct {
	Index   int      `json:"index"`
	Section []string `json:"section"` // heading path, title first
	Text    string   `json:"text"`    // what gets embedded and shown to the model
	Start   int      `json:"start"`   // byte range of the source text in Document.Text
	End     int      `json:"end"`
}

func Split(doc *parse.Document, cfg Config) []Chunk {
	var out []Chunk
	if cfg.Strategy == "fixed" {
		out = fixed(doc, cfg)
	} else {
		out = structured(doc, cfg)
	}
	for i := range out {
		out[i].Index = i
		if cfg.ContextHeader {
			out[i].Text = header(doc, out[i].Section) + out[i].Text
		}
	}
	return out
}

func header(doc *parse.Document, section []string) string {
	parts := append([]string{}, section...)
	if len(parts) == 0 || parts[0] != doc.Title {
		parts = append([]string{doc.Title}, parts...)
	}
	return strings.Join(parts, " > ") + "\n"
}

// sectionAt returns the heading path in force at a byte offset.
func sectionAt(doc *parse.Document, off int) []string {
	var path []string
	var levels []int
	for _, b := range doc.Blocks {
		if b.Start > off {
			break
		}
		if b.Kind != parse.Heading {
			continue
		}
		for len(levels) > 0 && levels[len(levels)-1] >= b.Level {
			levels, path = levels[:len(levels)-1], path[:len(path)-1]
		}
		levels, path = append(levels, b.Level), append(path, b.Text)
	}
	return path
}

// ---- fixed window ----

var wordRe = regexp.MustCompile(`\S+`)

func fixed(doc *parse.Document, cfg Config) []Chunk {
	words := wordRe.FindAllStringIndex(doc.Text, -1)
	if len(words) == 0 {
		return nil
	}
	// Convert the token budget into words using the document's own ratio.
	ratio := float64(parse.EstimateTokens(doc.Text)) / float64(len(words))
	perChunk := max(1, int(float64(cfg.Size)/ratio))
	step := max(1, perChunk-int(float64(cfg.Overlap)/ratio))

	var out []Chunk
	for i := 0; i < len(words); i += step {
		j := min(len(words), i+perChunk)
		start, end := words[i][0], words[j-1][1]
		out = append(out, Chunk{Section: sectionAt(doc, start), Text: doc.Text[start:end], Start: start, End: end})
		if j == len(words) {
			break
		}
	}
	return out
}

// ---- structure-aware ----

type piece struct {
	text       string
	start, end int
	prefix     string // repeated table header, not part of the source span
}

func structured(doc *parse.Document, cfg Config) []Chunk {
	var out []Chunk
	var cur []piece
	var curSection []string
	curTokens := 0

	emit := func() {
		if len(cur) == 0 {
			return
		}
		var sb strings.Builder
		for i, p := range cur {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(p.prefix + p.text)
		}
		out = append(out, Chunk{Section: curSection, Text: sb.String(), Start: cur[0].start, End: cur[len(cur)-1].end})

		// Carry the tail into the next chunk as overlap.
		var keep []piece
		tokens := 0
		for i := len(cur) - 1; i >= 0 && cfg.Overlap > 0; i-- {
			t := parse.EstimateTokens(cur[i].text)
			if tokens+t > cfg.Overlap {
				break
			}
			keep = append([]piece{cur[i]}, keep...)
			tokens += t
		}
		if len(keep) == len(cur) {
			keep = nil // never repeat a whole chunk
			tokens = 0
		}
		cur, curTokens = keep, tokens
	}

	for _, b := range doc.Blocks {
		if b.Kind == parse.Heading {
			emit()
			cur, curTokens = nil, 0
			curSection = sectionAt(doc, b.Start)
			continue
		}
		if curSection == nil {
			curSection = sectionAt(doc, b.Start)
		}
		for _, p := range split(b, cfg.Size) {
			t := parse.EstimateTokens(p.prefix + p.text)
			if curTokens > 0 && curTokens+t > cfg.Size {
				emit()
			}
			cur = append(cur, p)
			curTokens += t
		}
	}
	emit()
	return out
}

var sentenceEnd = regexp.MustCompile(`[.!?](\s+|$)`)

// split breaks one block into pieces no larger than size where possible:
// tables by rows (keeping the header), lists by items, prose by sentences.
func split(b parse.Block, size int) []piece {
	if parse.EstimateTokens(b.Text) <= size {
		return []piece{{text: b.Text, start: b.Start, end: b.End}}
	}
	var units [][2]int // byte ranges within b.Text
	var headerRow string
	switch b.Kind {
	case parse.Table, parse.List, parse.Code:
		off := 0
		for i, line := range strings.SplitAfter(b.Text, "\n") {
			if i == 0 && b.Kind == parse.Table {
				headerRow = strings.TrimRight(line, "\n") + "\n"
			} else {
				units = append(units, [2]int{off, off + len(strings.TrimRight(line, "\n"))})
			}
			off += len(line)
		}
	default:
		last := 0
		for _, m := range sentenceEnd.FindAllStringIndex(b.Text, -1) {
			units = append(units, [2]int{last, m[0] + 1})
			last = m[1]
		}
		if last < len(b.Text) {
			units = append(units, [2]int{last, len(b.Text)})
		}
	}

	var out []piece
	var from, to = -1, -1
	tokens := parse.EstimateTokens(headerRow)
	flush := func() {
		if from >= 0 {
			out = append(out, piece{text: b.Text[from:to], start: b.Start + from, end: b.Start + to, prefix: headerRow})
		}
		from, to, tokens = -1, -1, parse.EstimateTokens(headerRow)
	}
	for _, u := range units {
		t := parse.EstimateTokens(b.Text[u[0]:u[1]])
		if from >= 0 && tokens+t > size {
			flush()
		}
		if from < 0 {
			from = u[0]
		}
		to = u[1]
		tokens += t
	}
	flush()
	return out
}
