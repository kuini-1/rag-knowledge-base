// Package vector stores chunks in Qdrant (REST API) with a dense vector for
// semantic search and a sparse vector for keyword search.
package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kuini-1/rag-knowledge-base/internal/embed"
)

type Qdrant struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Qdrant {
	return &Qdrant{BaseURL: baseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Payload is stored with every point.
type Payload struct {
	Owner   string   `json:"owner"`
	DocID   string   `json:"doc_id"`
	DocName string   `json:"doc_name"`
	Title   string   `json:"title"`
	Section []string `json:"section"`
	Index   int      `json:"chunk_index"`
	Text    string   `json:"text"`
	Start   int      `json:"start"`
	End     int      `json:"end"`
}

type Point struct {
	ID      string
	Dense   []float32
	Sparse  embed.Sparse
	Payload Payload
}

type Hit struct {
	ID      string  `json:"id"`
	Score   float64 `json:"score"`
	Payload Payload `json:"payload"`
}

func (q *Qdrant) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.APIKey != "" {
		req.Header.Set("api-key", q.APIKey)
	}
	resp, err := q.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		return nil
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Result, out)
}

type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("qdrant: HTTP %d: %s", e.Status, e.Body) }

// EnsureCollection creates the collection if it doesn't exist, with a
// cosine dense vector, an IDF-weighted sparse vector, and payload indexes
// for the fields searches filter on.
func (q *Qdrant) EnsureCollection(ctx context.Context, name string, dim int) error {
	err := q.do(ctx, http.MethodGet, "/collections/"+name, nil, nil)
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); !ok || e.Status != http.StatusNotFound {
		return err
	}
	spec := map[string]any{
		"vectors":        map[string]any{"dense": map[string]any{"size": dim, "distance": "Cosine"}},
		"sparse_vectors": map[string]any{"sparse": map[string]any{"modifier": "idf"}},
	}
	if err := q.do(ctx, http.MethodPut, "/collections/"+name, spec, nil); err != nil {
		return err
	}
	for _, field := range []string{"owner", "doc_id"} {
		idx := map[string]any{"field_name": field, "field_schema": "keyword"}
		if err := q.do(ctx, http.MethodPut, "/collections/"+name+"/index?wait=true", idx, nil); err != nil {
			return err
		}
	}
	return nil
}

func (q *Qdrant) DeleteCollection(ctx context.Context, name string) error {
	err := q.do(ctx, http.MethodDelete, "/collections/"+name, nil, nil)
	if e, ok := err.(*Error); ok && e.Status == http.StatusNotFound {
		return nil
	}
	return err
}

func (q *Qdrant) Upsert(ctx context.Context, collection string, pts []Point) error {
	for i := 0; i < len(pts); i += 256 {
		batch := pts[i:min(len(pts), i+256)]
		body := make([]map[string]any, len(batch))
		for j, p := range batch {
			body[j] = map[string]any{
				"id":      p.ID,
				"vector":  map[string]any{"dense": p.Dense, "sparse": p.Sparse},
				"payload": p.Payload,
			}
		}
		if err := q.do(ctx, http.MethodPut, "/collections/"+collection+"/points?wait=true",
			map[string]any{"points": body}, nil); err != nil {
			return err
		}
	}
	return nil
}

// DeleteDoc removes every chunk of one document.
func (q *Qdrant) DeleteDoc(ctx context.Context, collection, owner, docID string) error {
	return q.do(ctx, http.MethodPost, "/collections/"+collection+"/points/delete?wait=true",
		map[string]any{"filter": ownerDocFilter(owner, docID)}, nil)
}

func ownerDocFilter(owner, docID string) map[string]any {
	must := []map[string]any{{"key": "owner", "match": map[string]any{"value": owner}}}
	if docID != "" {
		must = append(must, map[string]any{"key": "doc_id", "match": map[string]any{"value": docID}})
	}
	return map[string]any{"must": must}
}

// Search runs a dense-only search, or a hybrid search that fuses dense and
// sparse results with reciprocal rank fusion. Results are always limited to
// one owner's documents.
func (q *Qdrant) Search(ctx context.Context, collection, owner string, dense []float32, sparse *embed.Sparse, limit int) ([]Hit, error) {
	filter := ownerDocFilter(owner, "")
	var body map[string]any
	if sparse == nil {
		body = map[string]any{"query": dense, "using": "dense", "limit": limit, "filter": filter, "with_payload": true}
	} else {
		body = map[string]any{
			"prefetch": []map[string]any{
				{"query": dense, "using": "dense", "limit": limit * 2, "filter": filter},
				{"query": sparse, "using": "sparse", "limit": limit * 2, "filter": filter},
			},
			"query":        map[string]any{"fusion": "rrf"},
			"limit":        limit,
			"with_payload": true,
		}
	}
	var res struct {
		Points []Hit `json:"points"`
	}
	if err := q.do(ctx, http.MethodPost, "/collections/"+collection+"/points/query", body, &res); err != nil {
		return nil, err
	}
	return res.Points, nil
}
