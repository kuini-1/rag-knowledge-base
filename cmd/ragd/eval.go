package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/eval"
	"github.com/kuini-1/rag-knowledge-base/internal/parse"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

type evalFlags struct {
	dir       string
	embedURL  string
	qdrantURL string
	out       string
}

func (f *evalFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.dir, "eval-dir", "eval", "folder with corpus/ and dataset.jsonl")
	fs.StringVar(&f.embedURL, "embed-url", env("RAG_EMBED_URL", "http://127.0.0.1:8001"), "embedding service URL")
	fs.StringVar(&f.qdrantURL, "qdrant-url", env("RAG_QDRANT_URL", "http://127.0.0.1:6333"), "Qdrant URL")
	fs.StringVar(&f.out, "out", "eval/results", "where to write reports")
}

func (f *evalFlags) load() (map[string]*parse.Document, []eval.Question, error) {
	docs, err := eval.LoadCorpus(filepath.Join(f.dir, "corpus"))
	if err != nil {
		return nil, nil, err
	}
	qs, err := eval.LoadDataset(filepath.Join(f.dir, "dataset.jsonl"))
	if err != nil {
		return nil, nil, err
	}
	if err := eval.Validate(docs, qs); err != nil {
		return nil, nil, err
	}
	return docs, qs, nil
}

func cmdAblation(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ablation", flag.ExitOnError)
	var f evalFlags
	f.register(fs)
	base := fs.String("base", chunk.Default.Name(), "chunking config for the retrieval study (strategy-size-overlap[-ctx])")
	fs.Parse(args)

	baseCfg, err := parseChunkName(*base)
	if err != nil {
		return err
	}
	docs, qs, err := f.load()
	if err != nil {
		return err
	}
	models, err := serviceModels(ctx, f.embedURL)
	if err != nil {
		return err
	}
	store := vector.New(f.qdrantURL, os.Getenv("RAG_QDRANT_API_KEY"))
	rep, err := eval.Ablation(ctx, embed.New(f.embedURL), store, docs, qs, baseCfg, models, f.out,
		func(format string, a ...any) { fmt.Printf(format+"\n", a...) })
	if err != nil {
		return err
	}
	fmt.Printf("\nwrote %s/ablation.md (%d chunking configs, %d retrieval setups)\n", f.out, len(rep.Chunking), len(rep.Retrieval))
	return nil
}

// parseChunkName reads names like "structure-256-32-ctx" (see chunk.Config.Name).
func parseChunkName(s string) (chunk.Config, error) {
	c := chunk.Config{}
	if strings.HasSuffix(s, "-ctx") {
		c.ContextHeader = true
		s = strings.TrimSuffix(s, "-ctx")
	}
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return c, fmt.Errorf("chunk config %q: want strategy-size-overlap[-ctx]", s)
	}
	var err1, err2 error
	c.Strategy = parts[0]
	c.Size, err1 = strconv.Atoi(parts[1])
	c.Overlap, err2 = strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return c, fmt.Errorf("chunk config %q: size and overlap must be numbers", s)
	}
	return c, c.Validate()
}

// serviceModels asks the embedding service which models it runs.
func serviceModels(ctx context.Context, url string) ([2]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return [2]string{}, fmt.Errorf("embedding service at %s: %w", url, err)
	}
	defer resp.Body.Close()
	var h struct {
		Embed  string `json:"embed_model"`
		Rerank string `json:"rerank_model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return [2]string{}, err
	}
	return [2]string{h.Embed, h.Rerank}, nil
}
