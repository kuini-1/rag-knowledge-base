package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
)

// The question set must match the corpus exactly as the parsers read it,
// including the PDF and DOCX files.
func TestDatasetEvidenceExists(t *testing.T) {
	docs, err := LoadCorpus("../../eval/corpus")
	if err != nil {
		t.Fatal(err)
	}
	qs, err := LoadDataset("../../eval/dataset.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(docs, qs); err != nil {
		t.Fatal(err)
	}
}

// With chunks as large as whole documents, every evidence span must be
// covered by some chunk: the coverage check itself must not lose evidence.
func TestEveryEvidenceCoveredByWholeDocChunks(t *testing.T) {
	docs, _ := LoadCorpus("../../eval/corpus")
	qs, _ := LoadDataset("../../eval/dataset.jsonl")
	cfg := chunk.Config{Strategy: "structure", Size: 4096}
	for _, q := range qs {
		spans, _ := Locate(docs, q.Evidence)
		for _, s := range spans {
			found := false
			for _, c := range chunk.Split(docs[s.Doc], cfg) {
				if Covers(s.Doc, c.Start, c.End, s) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: evidence in %s not covered", q.ID, s.Doc)
			}
		}
	}
}

func TestPrintParsedStructure(t *testing.T) {
	if testing.Verbose() {
		docs, _ := LoadCorpus("../../eval/corpus")
		for _, name := range []string{"incident-report-2025-08.pdf", "data-retention.docx", "status-page.txt"} {
			d := docs[name]
			fmt.Printf("== %s (title %q, %d blocks)\n", name, d.Title, len(d.Blocks))
			for _, b := range d.Blocks {
				fmt.Printf("  %-9s L%d %q\n", b.Kind, b.Level, trunc(b.Text, 70))
			}
		}
	}
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " / ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
