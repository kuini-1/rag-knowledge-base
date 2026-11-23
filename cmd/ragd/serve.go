package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/answer"
	"github.com/kuini-1/rag-knowledge-base/internal/auth"
	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/eval"
	"github.com/kuini-1/rag-knowledge-base/internal/localllm"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
	"github.com/kuini-1/rag-knowledge-base/internal/server"
	"github.com/kuini-1/rag-knowledge-base/internal/store"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

type common struct {
	dataDir    string
	embedURL   string
	qdrantURL  string
	model      string
	effort     string
	extractive bool
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.dataDir, "data", env("RAG_DATA", "data"), "folder for the database and uploaded files")
	fs.StringVar(&c.embedURL, "embed-url", env("RAG_EMBED_URL", "http://127.0.0.1:8001"), "embedding service URL")
	fs.StringVar(&c.qdrantURL, "qdrant-url", env("RAG_QDRANT_URL", "http://127.0.0.1:6333"), "Qdrant URL")
	fs.StringVar(&c.model, "model", env("RAG_MODEL", answer.DefaultConfig.Model), "Claude model for answers")
	fs.StringVar(&c.effort, "effort", env("RAG_EFFORT", answer.DefaultConfig.Effort), "effort: low, medium, high")
	fs.BoolVar(&c.extractive, "extractive", env("RAG_ANSWER_MODE", "") == "extractive",
		"answer without a model: quote the best-matching source sentence (no API key needed)")
}

func (c *common) openStore() (*store.Store, error) {
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(c.dataDir, "ragd.db"))
}

func (c *common) answerer() *answer.Answerer {
	cfg := answer.DefaultConfig
	cfg.Model, cfg.Effort = c.model, c.effort
	if c.extractive {
		// No model: answers are the best-matching source sentence, cited.
		cfg.Model = "extractive"
		return &answer.Answerer{Cfg: cfg}
	}
	// Credentials come from ANTHROPIC_API_KEY (or an `ant auth login` profile).
	// Models named "ollama:<name>" run locally instead (see internal/localllm).
	return &answer.Answerer{LLM: router{local: &localllm.Client{BaseURL: os.Getenv("OLLAMA_HOST")}, claude: answer.SDKClient{Client: anthropic.NewClient()}}, Cfg: cfg}
}

// router sends each call to a local model or to Claude by the model's name,
// so answers and the eval's judge can each be either.
type router struct {
	local  answer.MessageClient
	claude answer.MessageClient
}

func (r router) New(ctx context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	if strings.HasPrefix(string(p.Model), localllm.Prefix) {
		return r.local.New(ctx, p)
	}
	return r.claude.New(ctx, p)
}

func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var c common
	c.register(fs)
	addr := fs.String("addr", env("RAG_ADDR", "127.0.0.1:8090"), "listen address")
	collection := fs.String("collection", env("RAG_COLLECTION", "chunks"), "Qdrant collection")
	chunking := fs.String("chunking", env("RAG_CHUNKING", chunk.Default.Name()), "chunking config (strategy-size-overlap[-ctx])")
	signup := fs.Bool("signup", env("RAG_ALLOW_SIGNUP", "true") == "true", "let anyone create an account (the first account is always allowed and becomes admin)")
	budget := fs.Int64("budget", int64(envInt("RAG_DEFAULT_BUDGET", 200_000)), "default monthly token budget per user")
	fs.Parse(args)

	chunkCfg, err := parseChunkName(*chunking)
	if err != nil {
		return err
	}
	st, err := c.openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	emb := embed.New(c.embedURL)
	dim, err := emb.Dim(ctx)
	if err != nil {
		return fmt.Errorf("the embedding service must be running: %w", err)
	}
	qd := vector.New(c.qdrantURL, os.Getenv("RAG_QDRANT_API_KEY"))
	if err := qd.EnsureCollection(ctx, *collection, dim); err != nil {
		return fmt.Errorf("qdrant: %w", err)
	}

	secret := []byte(os.Getenv("RAG_JWT_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
		slog.Warn("RAG_JWT_SECRET is not set: using a random secret, so sessions end when the server restarts")
	} else if len(secret) < 32 {
		return errors.New("RAG_JWT_SECRET must be at least 32 bytes")
	}

	srv := &server.Server{
		Store:         st,
		Retriever:     &retrieve.Retriever{Embed: emb, Store: qd, Collection: *collection},
		Answerer:      c.answerer(),
		Tokens:        auth.Tokens{Secret: secret, TTL: 12 * time.Hour},
		Chunking:      chunkCfg,
		Retrieval:     retrieve.DefaultOptions,
		FilesDir:      filepath.Join(c.dataDir, "files"),
		AllowSignup:   *signup,
		DefaultBudget: *budget,
		AskLimit:      auth.NewLimiter(20, 5),
		UploadLimit:   auth.NewLimiter(30, 10),
		LoginLimit:    auth.NewLimiter(10, 5),
	}
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hs.Shutdown(shut)
	}()
	slog.Info("ragd listening", "addr", *addr, "model", c.model, "chunking", chunkCfg.Name(), "collection", *collection)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func cmdUser(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return errors.New("usage: ragd user add -email you@example.com [-admin] [-budget 200000]")
	}
	fs := flag.NewFlagSet("user add", flag.ExitOnError)
	var c common
	c.register(fs)
	email := fs.String("email", "", "email address")
	admin := fs.Bool("admin", false, "give the user the admin role")
	budget := fs.Int64("budget", 200_000, "monthly token budget")
	fs.Parse(args[1:])

	addr, err := auth.NormalizeEmail(*email)
	if err != nil {
		return err
	}
	pw := os.Getenv("RAG_NEW_PASSWORD")
	if pw == "" {
		return errors.New("set the password in RAG_NEW_PASSWORD (kept out of shell history and process lists)")
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	st, err := c.openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	role := "user"
	if *admin {
		role = "admin"
	}
	u := &store.User{Email: addr, PasswordHash: hash, Role: role, TokenBudget: *budget}
	if err := st.CreateUser(ctx, u); err != nil {
		return err
	}
	fmt.Printf("created %s (%s, %s, budget %d tokens/month)\n", u.ID, u.Email, u.Role, u.TokenBudget)
	return nil
}

func cmdEval(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	var f evalFlags
	f.register(fs)
	var c common
	fs.StringVar(&c.model, "model", env("RAG_MODEL", answer.DefaultConfig.Model), "model for answers: a Claude model, or ollama:<name> for a local one")
	fs.StringVar(&c.effort, "effort", env("RAG_EFFORT", answer.DefaultConfig.Effort), "effort for answers")
	judge := fs.String("judge-model", env("RAG_JUDGE_MODEL", answer.DefaultConfig.Model), "model that grades answers: a Claude model, or ollama:<name>")
	chunking := fs.String("chunking", chunk.Default.Name(), "chunking config")
	limit := fs.Int("limit", 0, "only run the first N questions (0 = all)")
	fs.Parse(args)

	cfg, err := parseChunkName(*chunking)
	if err != nil {
		return err
	}
	docs, qs, err := f.load()
	if err != nil {
		return err
	}
	if *limit > 0 && *limit < len(qs) {
		qs = qs[:*limit]
	}
	emb := embed.New(f.embedURL)
	dim, err := emb.Dim(ctx)
	if err != nil {
		return err
	}
	r := &retrieve.Retriever{Embed: emb, Store: vector.New(f.qdrantURL, os.Getenv("RAG_QDRANT_API_KEY")), Collection: "eval_answers"}
	fmt.Printf("indexing %d documents with %s\n", len(docs), cfg.Name())
	if _, _, err := eval.IndexCorpus(ctx, r, docs, cfg, dim); err != nil {
		return err
	}
	a := c.answerer()
	if strings.HasPrefix(c.model, localllm.Prefix) && strings.HasPrefix(*judge, localllm.Prefix) {
		fmt.Printf("answering %d questions with %s, graded by %s (local models, no API calls)\n", len(qs), c.model, *judge)
	} else {
		fmt.Printf("answering %d questions with %s, graded by %s (this calls the Claude API and costs money)\n", len(qs), c.model, *judge)
	}
	rep, err := eval.Answers(ctx, r, a, a.LLM, *judge, qs, retrieve.DefaultOptions, f.out,
		func(format string, x ...any) { fmt.Printf(format+"\n", x...) })
	if err != nil {
		return err
	}
	fmt.Printf("\naccuracy %.0f%%, correct abstentions %d/%d, citation precision %.0f%% -> %s/answers.md\n",
		rep.Accuracy*100, rep.CorrectAbstains, rep.Unanswerable, rep.CitationPrecision*100, f.out)
	return nil
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return v
	}
	return def
}
