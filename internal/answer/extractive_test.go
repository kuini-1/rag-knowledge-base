package answer

import (
	"strings"
	"testing"

	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

func TestExtractivePicksTheMatchingSentenceAndCitesIt(t *testing.T) {
	srcs := []retrieve.Result{
		src("backups.md", 2, "Backups > Retention\nSnapshots run daily. Business: 30 days. Restores create a new volume.", 4),
		src("sla.md", 0, "SLA > Business\nThis plan commits to 99.95% monthly availability.", 1),
	}
	a := Extractive("How long are snapshots kept on Business?", srcs, 0.2)
	if !strings.Contains(a.Text, "30 days") {
		t.Errorf("answer = %q", a.Text)
	}
	if len(a.Segments[0].Citations) != 1 || a.Segments[0].Citations[0].DocName != "backups.md" {
		t.Errorf("citation = %+v", a.Segments[0].Citations)
	}
	if a.Check.Risk != "low" || a.Usage.Total() != 0 {
		t.Errorf("check = %+v usage = %+v", a.Check, a.Usage)
	}
}

func TestExtractiveAbstainsWithoutSources(t *testing.T) {
	a := Extractive("anything?", nil, 0.2)
	if !a.Check.Abstained {
		t.Errorf("expected an abstention, got %q", a.Text)
	}
}
