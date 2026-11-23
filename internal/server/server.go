// Package server is the HTTP API: accounts, document upload, cited answers,
// usage budgets and the query log.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kuini-1/rag-knowledge-base/internal/answer"
	"github.com/kuini-1/rag-knowledge-base/internal/auth"
	"github.com/kuini-1/rag-knowledge-base/internal/chunk"
	"github.com/kuini-1/rag-knowledge-base/internal/parse"
	"github.com/kuini-1/rag-knowledge-base/internal/retrieve"
	"github.com/kuini-1/rag-knowledge-base/internal/store"
)

type Server struct {
	Store         *store.Store
	Retriever     *retrieve.Retriever
	Answerer      *answer.Answerer
	Tokens        auth.Tokens
	Chunking      chunk.Config
	Retrieval     retrieve.Options
	FilesDir      string
	AllowSignup   bool
	DefaultBudget int64 // tokens per user per month
	Log           *slog.Logger

	AskLimit    *auth.Limiter // per user
	UploadLimit *auth.Limiter // per user
	LoginLimit  *auth.Limiter // per client IP

	indexSlots chan struct{}
}

func (s *Server) Handler() http.Handler {
	if s.Log == nil {
		s.Log = slog.Default()
	}
	s.indexSlots = make(chan struct{}, 2)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/auth/register", s.limitIP(s.register))
	mux.HandleFunc("POST /api/auth/login", s.limitIP(s.login))
	mux.HandleFunc("GET /api/me", s.authed(s.me))
	mux.HandleFunc("GET /api/documents", s.authed(s.listDocs))
	mux.HandleFunc("POST /api/documents", s.authed(s.limitUser(s.UploadLimit, s.upload)))
	mux.HandleFunc("DELETE /api/documents/{id}", s.authed(s.deleteDoc))
	mux.HandleFunc("POST /api/ask", s.authed(s.limitUser(s.AskLimit, s.ask)))
	mux.HandleFunc("GET /api/queries", s.authed(s.queries))
	mux.HandleFunc("POST /api/queries/{id}/feedback", s.authed(s.feedback))
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.authed(s.admin(s.setBudget)))
	return securityHeaders(mux)
}

// ---- middleware ----

type principal struct {
	ID   string
	Role string
}

func (s *Server) authed(next func(http.ResponseWriter, *http.Request, principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "sign in first")
			return
		}
		c, err := s.Tokens.Verify(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "session expired or invalid; sign in again")
			return
		}
		next(w, r, principal{ID: c.Subject, Role: c.Role})
	}
}

func (s *Server) admin(next func(http.ResponseWriter, *http.Request, principal)) func(http.ResponseWriter, *http.Request, principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal) {
		if p.Role != "admin" {
			writeErr(w, http.StatusForbidden, "admins only")
			return
		}
		next(w, r, p)
	}
}

func (s *Server) limitUser(l *auth.Limiter, next func(http.ResponseWriter, *http.Request, principal)) func(http.ResponseWriter, *http.Request, principal) {
	return func(w http.ResponseWriter, r *http.Request, p principal) {
		if l != nil {
			if ok, wait := l.Allow(p.ID); !ok {
				tooMany(w, wait, "rate limit reached")
				return
			}
		}
		next(w, r, p)
	}
}

func (s *Server) limitIP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.LoginLimit != nil {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			if ok, wait := s.LoginLimit.Allow(host); !ok {
				tooMany(w, wait, "too many attempts")
				return
			}
		}
		next(w, r)
	}
}

func tooMany(w http.ResponseWriter, wait time.Duration, msg string) {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("%s; try again in %d s", msg, secs))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// ---- accounts ----

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type session struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      *store.User `json:"user"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	n, err := s.Store.CountUsers(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The first account can always be created; it becomes the admin.
	if !s.AllowSignup && n > 0 {
		writeErr(w, http.StatusForbidden, "sign-up is closed; ask an admin for an account")
		return
	}
	email, err := auth.NormalizeEmail(c.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u := &store.User{Email: email, PasswordHash: hash, Role: "user", TokenBudget: s.DefaultBudget}
	if n == 0 {
		u.Role = "admin"
	}
	if err := s.Store.CreateUser(ctx, u); errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, "an account with this email already exists")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.issue(w, u, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	email, _ := auth.NormalizeEmail(c.Email)
	u, err := s.Store.UserByEmail(r.Context(), email)
	if err != nil {
		auth.SpendLoginTime(c.Password)
		writeErr(w, http.StatusUnauthorized, auth.ErrInvalid.Error())
		return
	}
	if !auth.CheckPassword(u.PasswordHash, c.Password) {
		writeErr(w, http.StatusUnauthorized, auth.ErrInvalid.Error())
		return
	}
	s.issue(w, u, http.StatusOK)
}

func (s *Server) issue(w http.ResponseWriter, u *store.User, code int) {
	tok, exp, err := s.Tokens.Issue(u.ID, u.Role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, code, session{Token: tok, ExpiresAt: exp, User: u})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, p principal) {
	u, err := s.Store.UserByID(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "account not found")
		return
	}
	usage, err := s.Store.Usage(r.Context(), p.ID, time.Now())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	usage.Budget = u.TokenBudget
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "usage": usage})
}

func (s *Server) setBudget(w http.ResponseWriter, r *http.Request, _ principal) {
	var body struct {
		TokenBudget int64 `json:"token_budget"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.TokenBudget < 0 {
		writeErr(w, http.StatusBadRequest, "token_budget must be zero or more")
		return
	}
	if err := s.Store.SetBudget(r.Context(), r.PathValue("id"), body.TokenBudget); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token_budget": body.TokenBudget})
}

// ---- documents ----

func (s *Server) listDocs(w http.ResponseWriter, r *http.Request, p principal) {
	docs, err := s.Store.Documents(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

const maxUpload = parse.MaxBytes + 1<<20

func (s *Server) upload(w http.ResponseWriter, r *http.Request, p principal) {
	r.Body = http.MaxBytesReader(w, r.Body, 5*maxUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "send files as multipart form data (field \"file\"), up to 20 MB each")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 || len(files) > 5 {
		writeErr(w, http.StatusBadRequest, "send between 1 and 5 files in the \"file\" field")
		return
	}
	var out []store.Document
	for _, fh := range files {
		name := filepath.Base(fh.Filename)
		ext := strings.ToLower(filepath.Ext(name))
		if !supported(ext) {
			writeErr(w, http.StatusUnsupportedMediaType, fmt.Sprintf("%s: unsupported type (use %s)", name, strings.Join(parse.Formats, ", ")))
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, maxUpload))
		f.Close()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		d := &store.Document{Owner: p.ID, Name: name, Format: strings.TrimPrefix(ext, "."), Size: int64(len(data))}
		if err := s.Store.CreateDocument(r.Context(), d); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.saveFile(p.ID, d.ID, data); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		go s.index(p.ID, d.ID, name, data)
		out = append(out, *d)
	}
	writeJSON(w, http.StatusAccepted, out)
}

func supported(ext string) bool {
	for _, f := range parse.Formats {
		if f == ext {
			return true
		}
	}
	return false
}

// The original file is kept so documents can be re-indexed with a new
// chunking config without asking users to upload again.
func (s *Server) saveFile(owner, id string, data []byte) error {
	dir := filepath.Join(s.FilesDir, owner)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id), data, 0o600)
}

func (s *Server) index(owner, docID, name string, data []byte) {
	s.indexSlots <- struct{}{}
	defer func() { <-s.indexSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	t0 := time.Now()
	n, err := func() (int, error) {
		doc, err := parse.Parse(name, data)
		if err != nil {
			return 0, err
		}
		return s.Retriever.Index(ctx, owner, docID, doc, s.Chunking)
	}()
	if err != nil {
		s.Log.Warn("indexing failed", "doc", docID, "err", err)
	} else {
		s.Log.Info("indexed", "doc", docID, "chunks", n, "ms", time.Since(t0).Milliseconds())
	}
	if ferr := s.Store.FinishDocument(context.Background(), docID, n, err); ferr != nil {
		s.Log.Error("could not record indexing result", "doc", docID, "err", ferr)
	}
}

func (s *Server) deleteDoc(w http.ResponseWriter, r *http.Request, p principal) {
	id := r.PathValue("id")
	if err := s.Store.DeleteDocument(r.Context(), p.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "document not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Retriever.Store.DeleteDoc(r.Context(), s.Retriever.Collection, p.ID, id); err != nil {
		writeErr(w, http.StatusBadGateway, "deleted the record, but removing its chunks failed: "+err.Error())
		return
	}
	_ = os.Remove(filepath.Join(s.FilesDir, p.ID, id))
	w.WriteHeader(http.StatusNoContent)
}

// ---- asking ----

type askReq struct {
	Question string `json:"question"`
}

type askResp struct {
	QueryID string `json:"query_id"`
	*answer.Answer
	LatencyMs int64            `json:"latency_ms"`
	Usage     store.MonthUsage `json:"month_usage"`
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request, p principal) {
	ctx := r.Context()
	var req askReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" || len(req.Question) > 2000 {
		writeErr(w, http.StatusBadRequest, "question must be 1 to 2000 characters")
		return
	}
	u, err := s.Store.UserByID(ctx, p.ID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "account not found")
		return
	}
	month, err := s.Store.Usage(ctx, p.ID, time.Now())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if month.Tokens >= u.TokenBudget {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("monthly token budget used (%d of %d); it resets on the 1st", month.Tokens, u.TokenBudget))
		return
	}

	t0 := time.Now()
	sources, err := s.Retriever.Search(ctx, p.ID, req.Question, s.Retrieval)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "search failed: "+err.Error())
		return
	}
	var ans *answer.Answer
	if len(sources) == 0 {
		// Nothing indexed: answer without spending tokens.
		ans = &answer.Answer{
			Text:     "I couldn't find this in your documents. Upload some documents first.",
			Segments: []answer.Segment{{Text: "I couldn't find this in your documents. Upload some documents first."}},
			Sources:  []retrieve.Result{},
		}
		ans.Check = answer.Validate(ans.Segments, nil, 0)
	} else {
		ans, err = s.Answerer.Answer(ctx, req.Question, sources)
		if errors.Is(err, answer.ErrRefused) {
			writeErr(w, http.StatusUnprocessableEntity, "the model declined to answer this question")
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, "answer generation failed: "+err.Error())
			return
		}
	}
	latency := time.Since(t0).Milliseconds()

	tokens := ans.Usage.Total()
	if tokens > 0 {
		if err := s.Store.AddUsage(ctx, p.ID, time.Now(), tokens); err != nil {
			s.Log.Error("could not record usage", "user", p.ID, "err", err)
		}
	}
	var top []string
	for _, src := range sources {
		top = append(top, fmt.Sprintf("%s#%d", src.Payload.DocName, src.Payload.Index))
	}
	q := &store.Query{Owner: p.ID, Question: req.Question, Answer: ans.Text, Risk: ans.Check.Risk,
		Grounded: ans.Check.Grounded, Confidence: ans.Check.Confidence, TopSources: strings.Join(top, ", "),
		Tokens: tokens, LatencyMs: latency}
	if err := s.Store.LogQuery(ctx, q); err != nil {
		s.Log.Error("could not log query", "err", err)
	}
	month.Tokens += tokens
	month.Requests++
	month.Budget = u.TokenBudget
	writeJSON(w, http.StatusOK, askResp{QueryID: q.ID, Answer: ans, LatencyMs: latency, Usage: month})
}

func (s *Server) queries(w http.ResponseWriter, r *http.Request, p principal) {
	qs, err := s.Store.Queries(r.Context(), p.ID, r.URL.Query().Get("flagged") == "1", 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, qs)
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request, p principal) {
	var body struct {
		Helpful bool `json:"helpful"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	v := -1
	if body.Helpful {
		v = 1
	}
	if err := s.Store.SetFeedback(r.Context(), p.ID, r.PathValue("id"), v); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "query not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
