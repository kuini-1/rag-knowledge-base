// Package retrieve turns a question into ranked chunks: dense or hybrid
// search for candidates, then an optional cross-encoder rerank.
package retrieve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/parse"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

type Options struct {
	Hybrid     bool `json:"hybrid"`     // add keyword (sparse) search, fused with RRF
	Rerank     bool `json:"rerank"`     // rescore candidates with the cross-encoder
	Candidates int  `json:"candidates"` // how many to fetch before reranking
	TopK       int  `json:"top_k"`      // how many to return
}

var DefaultOptions = Options{Hybrid: true, Rerank: true, Candidates: 30, TopK: 6}

type Result struct {
	vector.Hit
	Rank        int      `json:"rank"`
	SearchScore float64  `json:"search_score"`
	RerankScore *float64 `json:"rerank_score,omitempty"`
}

type Retriever struct {
	Embed      *embed.Client
	Store      *vector.Qdrant
	Collection string
}

func (r *Retriever) Search(ctx context.Context, owner, question string, o Options) ([]Result, error) {
	dense, err := r.Embed.Query(ctx, question)
	if err != nil {
		return nil, err
	}
	var sparse *embed.Sparse
	if o.Hybrid {
		s := embed.SparseQuery(question)
		if len(s.Indices) > 0 {
			sparse = &s
		}
	}
	limit := o.TopK
	if o.Rerank && o.Candidates > limit {
		limit = o.Candidates
	}
	hits, err := r.Store.Search(ctx, r.Collection, owner, dense, sparse, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(hits))
	for i, h := range hits {
		out[i] = Result{Hit: h, SearchScore: h.Score}
	}

	if o.Rerank && len(out) > 1 {
		passages := make([]string, len(out))
		for i, h := range out {
			passages[i] = h.Payload.Text
		}
		scores, err := r.Embed.Rerank(ctx, question, passages)
		if err != nil {
			return nil, err
		}
		for i := range out {
			s := scores[i]
			out[i].RerankScore = &s
		}
		sort.SliceStable(out, func(i, j int) bool { return *out[i].RerankScore > *out[j].RerankScore })
	}
	if len(out) > o.TopK {
		out = out[:o.TopK]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

// Confidence turns the best rerank logit into a 0-1 value. Without rerank
// scores it returns -1 (unknown).
func Confidence(rs []Result) float64 {
	if len(rs) == 0 {
		return 0
	}
	if rs[0].RerankScore == nil {
		return -1
	}
	return 1 / (1 + math.Exp(-*rs[0].RerankScore))
}

// Index chunks, embeds and stores one document. Point ids are derived from
// the document id and chunk index, so re-indexing overwrites cleanly.
func (r *Retriever) Index(ctx context.Context, owner, docID string, doc *parse.Document, cfg chunk.Config) (int, error) {
	chunks := chunk.Split(doc, cfg)
	if len(chunks) == 0 {
		return 0, fmt.Errorf("%s produced no chunks", doc.Name)
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	vecs, err := r.Embed.Passages(ctx, texts)
	if err != nil {
		return 0, err
	}
	pts := make([]vector.Point, len(chunks))
	for i, c := range chunks {
		pts[i] = vector.Point{
			ID:     pointID(owner, docID, i),
			Dense:  vecs[i],
			Sparse: embed.SparseDoc(c.Text),
			Payload: vector.Payload{
				Owner: owner, DocID: docID, DocName: doc.Name, Title: doc.Title,
				Section: c.Section, Index: i, Text: c.Text, Start: c.Start, End: c.End,
			},
		}
	}
	if err := r.Store.DeleteDoc(ctx, r.Collection, owner, docID); err != nil {
		return 0, err
	}
	if err := r.Store.Upsert(ctx, r.Collection, pts); err != nil {
		return 0, err
	}
	return len(chunks), nil
}

// pointID makes a stable UUID-shaped id (Qdrant requires UUIDs or integers).
func pointID(owner, docID string, i int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", owner, docID, i)))
	h := hex.EncodeToString(sum[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
