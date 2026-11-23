package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/parse"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
)

const evalOwner = "eval"

// Ks are the cut-offs reported for hit rate.
var Ks = []int{1, 3, 5, 10}

type Metrics struct {
	Label        string             `json:"label"`
	Chunking     chunk.Config       `json:"chunking"`
	Retrieval    retrieve.Options   `json:"retrieval"`
	Questions    int                `json:"questions"`
	Chunks       int                `json:"chunks"`
	AvgTokens    float64            `json:"avg_chunk_tokens"`
	HitAt        map[int]float64    `json:"hit_at"`         // share of questions with evidence in the top k
	MRR          float64            `json:"mrr"`            // mean reciprocal rank of the first hit (top 10)
	ContextAt5   float64            `json:"context_tokens"` // tokens the top 5 chunks would put in the prompt
	ByTag        map[string]float64 `json:"hit1_by_tag"`
	LatencyP50Ms float64            `json:"latency_p50_ms"`
	Misses       []Miss             `json:"misses"`
}

// Miss is a question whose evidence was not in the top 5, with what came back instead.
type Miss struct {
	ID        string   `json:"id"`
	Question  string   `json:"question"`
	Expected  []string `json:"expected"`
	Tags      []string `json:"tags"`
	FirstHit  int      `json:"first_hit_rank"` // 0 if not in the top 10
	Retrieved []string `json:"retrieved"`
}

// IndexCorpus (re)builds a collection holding the whole corpus with one chunking config.
func IndexCorpus(ctx context.Context, r *retrieve.Retriever, docs map[string]*parse.Document, cfg chunk.Config, dim int) (chunks int, avgTokens float64, err error) {
	if err := r.Store.DeleteCollection(ctx, r.Collection); err != nil {
		return 0, 0, err
	}
	if err := r.Store.EnsureCollection(ctx, r.Collection, dim); err != nil {
		return 0, 0, err
	}
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	sort.Strings(names)
	tokens := 0
	for _, n := range names {
		cs := chunk.Split(docs[n], cfg)
		for _, c := range cs {
			tokens += parse.EstimateTokens(c.Text)
		}
		got, err := r.Index(ctx, evalOwner, n, docs[n], cfg)
		if err != nil {
			return 0, 0, err
		}
		chunks += got
	}
	if err := r.Store.WaitReady(ctx, r.Collection); err != nil {
		return 0, 0, err
	}
	return chunks, float64(tokens) / float64(max(chunks, 1)), nil
}

// Retrieval scores one retrieval setup against the answerable questions.
func Retrieval(ctx context.Context, r *retrieve.Retriever, docs map[string]*parse.Document, qs []Question, label string, opts retrieve.Options) (*Metrics, error) {
	opts.TopK = 10
	m := &Metrics{Label: label, Retrieval: opts, HitAt: map[int]float64{}, ByTag: map[string]float64{}}
	tagTotal := map[string]int{}
	tagHits := map[string]int{}
	var latencies []float64
	ctxTokens := 0

	for _, q := range qs {
		if !q.Answerable {
			continue
		}
		spans, err := Locate(docs, q.Evidence)
		if err != nil {
			return nil, err
		}
		t0 := time.Now()
		res, err := r.Search(ctx, evalOwner, q.Question, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", q.ID, err)
		}
		latencies = append(latencies, float64(time.Since(t0).Microseconds())/1000)
		m.Questions++

		first := firstSatisfied(res, spans, q.EvidenceMode)
		for _, k := range Ks {
			if first > 0 && first <= k {
				m.HitAt[k]++
			}
		}
		if first > 0 {
			m.MRR += 1 / float64(first)
		}
		for i := 0; i < len(res) && i < 5; i++ {
			ctxTokens += parse.EstimateTokens(res[i].Payload.Text)
		}
		tags := q.Tags
		if len(tags) == 0 {
			tags = []string{"plain"}
		}
		for _, t := range tags {
			tagTotal[t]++
			if first == 1 {
				tagHits[t]++
			}
		}
		if first == 0 || first > 5 {
			miss := Miss{ID: q.ID, Question: q.Question, Tags: q.Tags, FirstHit: first}
			for _, e := range q.Evidence {
				miss.Expected = append(miss.Expected, e.Doc+": "+e.Text)
			}
			for i := 0; i < len(res) && i < 3; i++ {
				p := res[i].Payload
				miss.Retrieved = append(miss.Retrieved, fmt.Sprintf("#%d %s > %s: %s", i+1, p.DocName,
					strings.Join(p.Section, " > "), oneLine(p.Text, 160)))
			}
			m.Misses = append(m.Misses, miss)
		}
	}
	n := float64(max(m.Questions, 1))
	for _, k := range Ks {
		m.HitAt[k] /= n
	}
	m.MRR /= n
	m.ContextAt5 = float64(ctxTokens) / n
	for t, total := range tagTotal {
		m.ByTag[t] = float64(tagHits[t]) / float64(total)
	}
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		m.LatencyP50Ms = latencies[len(latencies)/2]
	}
	return m, nil
}

// firstSatisfied returns the 1-based rank at which the evidence requirement
// is met ("any": one span covered; "all": every span covered by the results
// so far), or 0.
func firstSatisfied(res []retrieve.Result, spans []Span, mode string) int {
	covered := make([]bool, len(spans))
	for i, r := range res {
		p := r.Payload
		for j, s := range spans {
			if Covers(p.DocName, p.Start, p.End, s) {
				covered[j] = true
			}
		}
		if mode == "all" {
			all := true
			for _, c := range covered {
				all = all && c
			}
			if all {
				return i + 1
			}
		} else {
			for _, c := range covered {
				if c {
					return i + 1
				}
			}
		}
	}
	return 0
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
