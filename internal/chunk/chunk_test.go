package chunk

import (
	"strings"
	"testing"

	"github.com/kuini-1/rag-knowledge-base/internal/parse"
)

const md = `# Pricing

Intro paragraph about plans.

## Plans

| Plan | Price |
|---|---|
| Starter | $0 |
| Team | $49 |
| Business | $399 |
| Enterprise | Custom |

## Overage

This plan bills extra storage monthly. Egress between regions costs half. Seats cost extra on Team.
`

func doc(t *testing.T) *parse.Document {
	t.Helper()
	d, err := parse.Markdown(md)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Every chunk's byte range must point at text that really is in the
// document, and its content must come from that range.
func TestOffsetsMapBackToSource(t *testing.T) {
	d := doc(t)
	for _, cfg := range []Config{
		{Strategy: "fixed", Size: 32, Overlap: 8},
		{Strategy: "structure", Size: 32, Overlap: 8},
		{Strategy: "structure", Size: 32, ContextHeader: true},
	} {
		for _, c := range Split(d, cfg) {
			if c.Start < 0 || c.End > len(d.Text) || c.Start >= c.End {
				t.Fatalf("%s: bad range %d-%d", cfg.Name(), c.Start, c.End)
			}
			src := d.Text[c.Start:c.End]
			first := strings.SplitN(src, "\n", 2)[0]
			if !strings.Contains(c.Text, strings.TrimSpace(first)) {
				t.Errorf("%s: chunk text doesn't contain the start of its source range %q", cfg.Name(), first)
			}
		}
	}
}

func TestStructureNeverCrossesHeadings(t *testing.T) {
	for _, c := range Split(doc(t), Config{Strategy: "structure", Size: 1000}) {
		if strings.Contains(c.Text, "Starter") && strings.Contains(c.Text, "Egress") {
			t.Errorf("chunk spans two sections: %q", c.Text)
		}
	}
}

func TestSplitTablesRepeatTheHeader(t *testing.T) {
	var tableChunks []Chunk
	for _, c := range Split(doc(t), Config{Strategy: "structure", Size: 12}) {
		if strings.Contains(c.Text, "| $") {
			tableChunks = append(tableChunks, c)
		}
	}
	if len(tableChunks) < 2 {
		t.Fatalf("expected the table to be split, got %d chunk(s)", len(tableChunks))
	}
	for _, c := range tableChunks {
		if !strings.HasPrefix(c.Text, "| Plan | Price |") {
			t.Errorf("table piece without its header: %q", c.Text)
		}
	}
}

func TestContextHeaderNamesTheSection(t *testing.T) {
	for _, c := range Split(doc(t), Config{Strategy: "structure", Size: 256, ContextHeader: true}) {
		if strings.Contains(c.Text, "This plan bills") && !strings.HasPrefix(c.Text, "Pricing > Overage\n") {
			t.Errorf("missing context header: %q", c.Text)
		}
	}
}

func TestFixedOverlap(t *testing.T) {
	cs := Split(doc(t), Config{Strategy: "fixed", Size: 16, Overlap: 6})
	if len(cs) < 3 {
		t.Fatalf("got %d chunks", len(cs))
	}
	for i := 1; i < len(cs); i++ {
		if cs[i].Start >= cs[i-1].End {
			t.Errorf("chunk %d does not overlap the previous one", i)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := []Config{{Strategy: "semantic", Size: 256}, {Strategy: "fixed", Size: 8}, {Strategy: "fixed", Size: 64, Overlap: 40}}
	for _, c := range bad {
		if c.Validate() == nil {
			t.Errorf("%+v should be invalid", c)
		}
	}
	if Default.Validate() != nil {
		t.Error("default config should be valid")
	}
}
