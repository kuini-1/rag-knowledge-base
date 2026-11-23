package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/parse"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

// The chunking study holds retrieval fixed (dense only, no rerank) so that
// only the chunking changes between rows.
var ChunkingConfigs = []chunk.Config{
	{Strategy: "fixed", Size: 64, Overlap: 0},
	{Strategy: "fixed", Size: 128, Overlap: 0},
	{Strategy: "fixed", Size: 256, Overlap: 0},
	{Strategy: "fixed", Size: 512, Overlap: 0},
	{Strategy: "fixed", Size: 128, Overlap: 32},
	{Strategy: "fixed", Size: 256, Overlap: 64},
	{Strategy: "structure", Size: 128, Overlap: 0},
	{Strategy: "structure", Size: 256, Overlap: 0},
	{Strategy: "structure", Size: 128, Overlap: 0, ContextHeader: true},
	{Strategy: "structure", Size: 256, Overlap: 0, ContextHeader: true},
	{Strategy: "structure", Size: 256, Overlap: 32, ContextHeader: true},
	{Strategy: "structure", Size: 512, Overlap: 0, ContextHeader: true},
}

// The retrieval study runs on one chunking config and varies the search.
var RetrievalConfigs = []struct {
	Label string
	Opts  retrieve.Options
}{
	{"dense", retrieve.Options{Candidates: 30}},
	{"hybrid (dense + keyword, RRF)", retrieve.Options{Hybrid: true, Candidates: 30}},
	{"dense + rerank", retrieve.Options{Rerank: true, Candidates: 30}},
	{"hybrid + rerank", retrieve.Options{Hybrid: true, Rerank: true, Candidates: 30}},
}

type AblationReport struct {
	Date        string     `json:"date"`
	EmbedModel  string     `json:"embed_model"`
	RerankModel string     `json:"rerank_model"`
	Documents   int        `json:"documents"`
	Questions   int        `json:"questions"`
	Chunking    []*Metrics `json:"chunking"`
	Retrieval   []*Metrics `json:"retrieval"`
	BaseConfig  string     `json:"retrieval_base_config"`
}

// Ablation runs both studies and writes ablation.md, ablation.json and
// bad_retrievals.jsonl into outDir.
func Ablation(ctx context.Context, emb *embed.Client, store *vector.Qdrant, docs map[string]*parse.Document, qs []Question,
	base chunk.Config, models [2]string, outDir string, logf func(string, ...any)) (*AblationReport, error) {
	dim, err := emb.Dim(ctx)
	if err != nil {
		return nil, err
	}
	rep := &AblationReport{Date: time.Now().Format("2006-01-02"), EmbedModel: models[0], RerankModel: models[1],
		Documents: len(docs), BaseConfig: base.Name()}

	for _, cfg := range ChunkingConfigs {
		r := &retrieve.Retriever{Embed: emb, Store: store, Collection: "eval_" + strings.ReplaceAll(cfg.Name(), "-", "_")}
		n, avg, err := IndexCorpus(ctx, r, docs, cfg, dim)
		if err != nil {
			return nil, err
		}
		m, err := Retrieval(ctx, r, docs, qs, cfg.Name(), retrieve.Options{Candidates: 30})
		if err != nil {
			return nil, err
		}
		m.Chunking, m.Chunks, m.AvgTokens = cfg, n, avg
		rep.Chunking = append(rep.Chunking, m)
		rep.Questions = m.Questions
		logf("%-28s chunks=%-4d hit@1=%.2f hit@5=%.2f mrr=%.3f", cfg.Name(), n, m.HitAt[1], m.HitAt[5], m.MRR)
		_ = store.DeleteCollection(ctx, r.Collection)
	}

	r := &retrieve.Retriever{Embed: emb, Store: store, Collection: "eval_base"}
	n, avg, err := IndexCorpus(ctx, r, docs, base, dim)
	if err != nil {
		return nil, err
	}
	for _, rc := range RetrievalConfigs {
		m, err := Retrieval(ctx, r, docs, qs, rc.Label, rc.Opts)
		if err != nil {
			return nil, err
		}
		m.Chunking, m.Chunks, m.AvgTokens = base, n, avg
		rep.Retrieval = append(rep.Retrieval, m)
		logf("%-28s hit@1=%.2f hit@5=%.2f mrr=%.3f p50=%.0fms", rc.Label, m.HitAt[1], m.HitAt[5], m.MRR, m.LatencyP50Ms)
	}
	_ = store.DeleteCollection(ctx, r.Collection)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	js, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "ablation.json"), js, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(outDir, "ablation.md"), []byte(rep.Markdown()), 0o644); err != nil {
		return nil, err
	}
	var lines []string
	for _, m := range append(append([]*Metrics{}, rep.Chunking...), rep.Retrieval...) {
		for _, miss := range m.Misses {
			b, _ := json.Marshal(map[string]any{"setup": m.Label, "miss": miss})
			lines = append(lines, string(b))
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "bad_retrievals.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}
	return rep, nil
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

func (r *AblationReport) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Retrieval ablation\n\nRun on %s. %d documents, %d answerable questions. Embeddings: `%s`. Reranker: `%s`.\n\n",
		r.Date, r.Documents, r.Questions, r.EmbedModel, r.RerankModel)
	sb.WriteString("A question counts as a hit at k when a chunk among the top k covers at least 80% of the passage that answers it. MRR is over the top 10.\n\n")

	sb.WriteString("## Chunking (dense search, no rerank)\n\n")
	sb.WriteString("| Config | Chunks | Avg tokens | Hit@1 | Hit@3 | Hit@5 | MRR | Prompt tokens (top 5) |\n|---|---|---|---|---|---|---|---|\n")
	for _, m := range r.Chunking {
		fmt.Fprintf(&sb, "| `%s` | %d | %.0f | %s | %s | %s | %.3f | %.0f |\n", m.Label, m.Chunks, m.AvgTokens,
			pct(m.HitAt[1]), pct(m.HitAt[3]), pct(m.HitAt[5]), m.MRR, m.ContextAt5)
	}

	fmt.Fprintf(&sb, "\n## Retrieval (chunking `%s`)\n\n", r.BaseConfig)
	sb.WriteString("| Setup | Hit@1 | Hit@3 | Hit@5 | MRR | Latency p50 |\n|---|---|---|---|---|---|\n")
	for _, m := range r.Retrieval {
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %.3f | %.0f ms |\n", m.Label, pct(m.HitAt[1]), pct(m.HitAt[3]), pct(m.HitAt[5]), m.MRR, m.LatencyP50Ms)
	}

	sb.WriteString("\n## Hit@1 by question type\n\n")
	tags := map[string]bool{}
	for _, m := range r.Retrieval {
		for t := range m.ByTag {
			tags[t] = true
		}
	}
	var ts []string
	for t := range tags {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	sb.WriteString("Share of questions whose answer is in the first result, per tag.\n\n| Tag | " + strings.Join(labels(r.Retrieval), " | ") + " |\n|---|" + strings.Repeat("---|", len(r.Retrieval)) + "\n")
	for _, t := range ts {
		row := []string{t}
		for _, m := range r.Retrieval {
			row = append(row, pct(m.ByTag[t]))
		}
		sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString("\nEvery miss (evidence not in the top 5) is listed with what was retrieved instead in `bad_retrievals.jsonl`.\n")
	return sb.String()
}

func labels(ms []*Metrics) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Label)
	}
	return out
}
