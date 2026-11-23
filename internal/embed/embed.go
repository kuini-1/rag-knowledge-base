// Package embed calls the embedding service (embedder/app.py) for dense
// vectors and cross-encoder rerank scores, and builds sparse keyword vectors
// locally for hybrid search.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Batch   int
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 5 * time.Minute}, Batch: 64}
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("embedding service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("embedding service %s: %s: %s", path, resp.Status, msg)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Passages embeds document chunks, batching requests.
func (c *Client) Passages(ctx context.Context, texts []string) ([][]float32, error) {
	return c.embed(ctx, texts, "passage")
}

// Query embeds a search query (the model adds its query instruction).
func (c *Client) Query(ctx context.Context, text string) ([]float32, error) {
	v, err := c.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, err
	}
	return v[0], nil
}

func (c *Client) embed(ctx context.Context, texts []string, kind string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += c.Batch {
		batch := texts[i:min(len(texts), i+c.Batch)]
		var resp struct {
			Vectors [][]float32 `json:"vectors"`
		}
		if err := c.post(ctx, "/embed", map[string]any{"texts": batch, "kind": kind}, &resp); err != nil {
			return nil, err
		}
		if len(resp.Vectors) != len(batch) {
			return nil, fmt.Errorf("embedding service returned %d vectors for %d texts", len(resp.Vectors), len(batch))
		}
		out = append(out, resp.Vectors...)
	}
	return out, nil
}

// Rerank scores each passage against the query with the cross-encoder.
// Higher is more relevant; scores are logits, not probabilities.
func (c *Client) Rerank(ctx context.Context, query string, passages []string) ([]float64, error) {
	var resp struct {
		Scores []float64 `json:"scores"`
	}
	if err := c.post(ctx, "/rerank", map[string]any{"query": query, "passages": passages}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Scores) != len(passages) {
		return nil, fmt.Errorf("rerank returned %d scores for %d passages", len(resp.Scores), len(passages))
	}
	return resp.Scores, nil
}

// Dim asks the service for the embedding size.
func (c *Client) Dim(ctx context.Context) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("embedding service: %w", err)
	}
	defer resp.Body.Close()
	var h struct {
		Dim int `json:"dim"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return 0, err
	}
	return h.Dim, nil
}
