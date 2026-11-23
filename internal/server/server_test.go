package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/kuini-1/rag-knowledge-base/internal/answer"
	"github.com/kuini-1/rag-knowledge-base/internal/auth"
	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/embed"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
	"github.com/kuini-1/rag-knowledge-base/internal/store"
	"github.com/kuini-1/rag-knowledge-base/internal/vector"
)

// fakeLLM answers by citing the first search result in the request.
type fakeLLM struct{}

func (fakeLLM) New(_ context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	first := p.Messages[0].OfUser.Content[0].OfSearchResult.Content[0].Text
	quote := strings.SplitN(first, "\n", 3)[1] // skip the context header line
	raw, _ := json.Marshal(map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": "claude-opus-5-5", "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": quote, "citations": []map[string]any{{
			"type": "search_result_location", "search_result_index": 0, "cited_text": quote,
			"source": "s", "title": "t", "start_block_index": 0, "end_block_index": 1,
		}}}},
		"usage": map[string]any{"input_tokens": 700, "output_tokens": 50},
	})
	var m anthropic.BetaMessage
	return &m, json.Unmarshal(raw, &m)
}

// TestAPI needs Qdrant and the embedding service running:
//
//	RAG_INTEGRATION=1 go test ./internal/server/
func TestAPI(t *testing.T) {
	if os.Getenv("RAG_INTEGRATION") == "" {
		t.Skip("set RAG_INTEGRATION=1 with Qdrant and the embedder running")
	}
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	emb := embed.New(envOr("RAG_EMBED_URL", "http://127.0.0.1:8001"))
	qd := vector.New(envOr("RAG_QDRANT_URL", "http://127.0.0.1:6333"), "")
	collection := "test_" + store.NewID("")
	dim, err := emb.Dim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := qd.EnsureCollection(ctx, collection, dim); err != nil {
		t.Fatal(err)
	}
	defer qd.DeleteCollection(ctx, collection)

	srv := &Server{
		Store: st, Retriever: &retrieve.Retriever{Embed: emb, Store: qd, Collection: collection},
		Answerer: &answer.Answerer{LLM: fakeLLM{}, Cfg: answer.DefaultConfig},
		Tokens:   auth.Tokens{Secret: []byte(strings.Repeat("k", 32)), TTL: time.Hour},
		Chunking: chunk.Default, Retrieval: retrieve.DefaultOptions, FilesDir: filepath.Join(dir, "files"),
		AllowSignup: true, DefaultBudget: 2000,
		// Slow refill (one ask per 10 s) so slow requests can't refill the
		// bucket during the rate-limit check; burst 4 covers Ana's asks.
		AskLimit: auth.NewLimiter(6, 4), LoginLimit: auth.NewLimiter(600, 50),
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	call := func(method, path, token string, body any, out any) int {
		t.Helper()
		var rd io.Reader
		ct := "application/json"
		switch b := body.(type) {
		case nil:
		case *bytes.Buffer:
			rd, ct = b, "" // multipart sets its own type below
		default:
			buf, _ := json.Marshal(b)
			rd = bytes.NewReader(buf)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if _, ok := body.(*bytes.Buffer); ok {
			req.Header.Set("Content-Type", uploadContentType)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}

	// The first account becomes admin; weak passwords are rejected.
	var admin, bob session
	if code := call("POST", "/api/auth/register", "", credentials{"Ana@Example.com", "short"}, nil); code != 400 {
		t.Fatalf("weak password: %d", code)
	}
	if code := call("POST", "/api/auth/register", "", credentials{"ana@example.com", "correct horse battery"}, &admin); code != 201 || admin.User.Role != "admin" {
		t.Fatalf("register: %d %+v", code, admin.User)
	}
	if code := call("POST", "/api/auth/register", "", credentials{"bob@example.com", "another long password"}, &bob); code != 201 || bob.User.Role != "user" {
		t.Fatalf("register bob: %d", code)
	}
	if code := call("POST", "/api/auth/login", "", credentials{"ana@example.com", "wrong password!!"}, nil); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
	if code := call("GET", "/api/documents", "", nil, nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}

	// Upload a document and wait for indexing.
	body := multipartFile(t, "limits.md", "# Limits\n\n## Rate limits\n\nThe Team plan allows 1,200 requests per minute.\n")
	var docs []store.Document
	if code := call("POST", "/api/documents", admin.Token, body, &docs); code != 202 || len(docs) != 1 {
		t.Fatalf("upload: %d %+v", code, docs)
	}
	waitReady(t, func() []store.Document {
		var d []store.Document
		call("GET", "/api/documents", admin.Token, nil, &d)
		return d
	})

	// A cited, checked answer; usage is counted.
	var ans struct {
		QueryID string           `json:"query_id"`
		Text    string           `json:"text"`
		Check   answer.Check     `json:"check"`
		Usage   store.MonthUsage `json:"month_usage"`
	}
	if code := call("POST", "/api/ask", admin.Token, askReq{"What is the Team rate limit?"}, &ans); code != 200 {
		t.Fatalf("ask: %d", code)
	}
	if !strings.Contains(ans.Text, "1,200") || ans.Check.Risk != "low" || ans.Usage.Tokens != 750 {
		t.Fatalf("answer = %+v", ans)
	}

	// Bob can't see Ana's documents: his search finds nothing and costs nothing.
	var bobAns struct {
		Text  string           `json:"text"`
		Usage store.MonthUsage `json:"month_usage"`
	}
	call("POST", "/api/ask", bob.Token, askReq{"What is the Team rate limit?"}, &bobAns)
	if strings.Contains(bobAns.Text, "1,200") || bobAns.Usage.Tokens != 0 {
		t.Fatalf("bob saw another user's data: %+v", bobAns)
	}
	if code := call("PATCH", "/api/admin/users/"+admin.User.ID, bob.Token, map[string]int{"token_budget": 1}, nil); code != 403 {
		t.Fatalf("non-admin changed a budget: %d", code)
	}

	// Budget: 750 + 750 + 750 crosses 2000, so the fourth ask is refused.
	call("POST", "/api/ask", admin.Token, askReq{"Team rate limit again?"}, nil)
	call("POST", "/api/ask", admin.Token, askReq{"And once more?"}, nil)
	var e struct{ Error string }
	if code := call("POST", "/api/ask", admin.Token, askReq{"One more?"}, &e); code != 429 || !strings.Contains(e.Error, "budget") {
		t.Fatalf("budget not enforced: %d %s", code, e.Error)
	}

	// Rate limit (burst 4): rapid asks from Bob get 429 with Retry-After.
	limited := false
	for i := 0; i < 6; i++ {
		if call("POST", "/api/ask", bob.Token, askReq{"hello?"}, nil) == 429 {
			limited = true
		}
	}
	if !limited {
		t.Fatal("rate limit never triggered")
	}

	// Feedback marks the query for review.
	if code := call("POST", "/api/queries/"+ans.QueryID+"/feedback", admin.Token, map[string]bool{"helpful": false}, nil); code != 204 {
		t.Fatalf("feedback: %d", code)
	}
	var flagged []store.Query
	call("GET", "/api/queries?flagged=1", admin.Token, nil, &flagged)
	if len(flagged) != 1 || flagged[0].ID != ans.QueryID {
		t.Fatalf("flagged = %+v", flagged)
	}

	// Deleting removes the chunks too.
	if code := call("DELETE", "/api/documents/"+docs[0].ID, admin.Token, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	hits, _ := srv.Retriever.Search(ctx, admin.User.ID, "Team rate limit", retrieve.Options{TopK: 5})
	if len(hits) != 0 {
		t.Fatalf("chunks left after delete: %d", len(hits))
	}
}

var uploadContentType string

func multipartFile(t *testing.T, name, content string) *bytes.Buffer {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", name)
	fw.Write([]byte(content))
	w.Close()
	uploadContentType = w.FormDataContentType()
	return &buf
}

func waitReady(t *testing.T, list func() []store.Document) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		d := list()
		if len(d) > 0 && d[0].Status == store.DocReady {
			return
		}
		if len(d) > 0 && d[0].Status == store.DocFailed {
			t.Fatalf("indexing failed: %s", d[0].Error)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("document never became ready")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
