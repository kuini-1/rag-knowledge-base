// Package eval measures retrieval and answer quality against a question set.
package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kuini-1/rag-knowledge-base/internal/parse"
)

type Evidence struct {
	Doc  string `json:"doc"`
	Text string `json:"text"`
}

type Question struct {
	ID           string     `json:"id"`
	Question     string     `json:"question"`
	Answer       *string    `json:"answer"` // nil when the corpus can't answer it
	Evidence     []Evidence `json:"evidence"`
	EvidenceMode string     `json:"evidence_mode"` // "any" or "all"
	Tags         []string   `json:"tags"`
	Answerable   bool       `json:"answerable"`
}

func LoadDataset(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Question
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var q Question
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n, err)
		}
		out = append(out, q)
	}
	return out, sc.Err()
}

// LoadCorpus parses every supported file in dir, keyed by file name.
func LoadCorpus(dir string) (map[string]*parse.Document, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	docs := map[string]*parse.Document{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		d, err := parse.Parse(e.Name(), data)
		if err != nil {
			return nil, err
		}
		docs[e.Name()] = d
	}
	return docs, nil
}

// Span is a byte range of evidence inside a document's text.
type Span struct {
	Doc        string
	Start, End int
}

// Locate finds each evidence passage in its document, ignoring differences
// in whitespace and case (PDF extraction re-flows lines).
func Locate(docs map[string]*parse.Document, ev []Evidence) ([]Span, error) {
	var out []Span
	for _, e := range ev {
		d, ok := docs[e.Doc]
		if !ok {
			return nil, fmt.Errorf("evidence refers to unknown document %q", e.Doc)
		}
		words := strings.Fields(e.Text)
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		re := regexp.MustCompile(`(?i)` + strings.Join(words, `\s+`))
		loc := re.FindStringIndex(d.Text)
		if loc == nil {
			return nil, fmt.Errorf("evidence not found in %s: %q", e.Doc, e.Text)
		}
		out = append(out, Span{Doc: e.Doc, Start: loc[0], End: loc[1]})
	}
	return out, nil
}

// Covers reports whether a chunk's source range holds at least 80% of the
// evidence span. Requiring most (not all) of it tolerates a boundary that
// cuts off a trailing word without counting a chunk that lost the fact.
func Covers(chunkDoc string, start, end int, s Span) bool {
	if chunkDoc != s.Doc {
		return false
	}
	overlap := min(end, s.End) - max(start, s.Start)
	return overlap > 0 && float64(overlap) >= 0.8*float64(s.End-s.Start)
}

// Validate checks that every answerable question's evidence exists.
func Validate(docs map[string]*parse.Document, qs []Question) error {
	var problems []string
	for _, q := range qs {
		if !q.Answerable {
			continue
		}
		if _, err := Locate(docs, q.Evidence); err != nil {
			problems = append(problems, q.ID+": "+err.Error())
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("%d questions have missing evidence:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	return nil
}
