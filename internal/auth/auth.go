// Package auth handles password hashing, signed session tokens and
// per-user rate limits.
package auth

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

var ErrInvalid = errors.New("invalid email or password")

func HashPassword(pw string) (string, error) {
	if len(pw) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	if len(pw) > 72 {
		return "", errors.New("password must be at most 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(h), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash lets failed logins for unknown emails take as long as real ones,
// so response time doesn't reveal which emails are registered.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), 12)

func SpendLoginTime(pw string) { _ = bcrypt.CompareHashAndPassword(dummyHash, []byte(pw)) }

func NormalizeEmail(s string) (string, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(s))
	if err != nil || a.Name != "" {
		return "", errors.New("invalid email address")
	}
	return strings.ToLower(a.Address), nil
}

// Tokens issues and verifies HS256 session tokens.
type Tokens struct {
	Secret []byte
	TTL    time.Duration
}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (t Tokens) Issue(userID, role string) (string, time.Time, error) {
	exp := time.Now().Add(t.TTL)
	c := Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{
		Subject: userID, ExpiresAt: jwt.NewNumericDate(exp), IssuedAt: jwt.NewNumericDate(time.Now()), Issuer: "ragd",
	}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.Secret)
	return s, exp, err
}

func (t Tokens) Verify(token string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(tok *jwt.Token) (any, error) {
		return t.Secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("ragd"), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}
	return &c, nil
}

// Limiter hands out one token bucket per key (user id or client IP).
type Limiter struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	buckets map[string]*entry
}

type entry struct {
	l    *rate.Limiter
	seen time.Time
}

// NewLimiter allows perMinute requests per minute with the given burst.
func NewLimiter(perMinute float64, burst int) *Limiter {
	l := &Limiter{every: rate.Limit(perMinute / 60), burst: burst, buckets: map[string]*entry{}}
	go l.gc()
	return l
}

// Allow reports whether the request may proceed and, if not, how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	e, ok := l.buckets[key]
	if !ok {
		e = &entry{l: rate.NewLimiter(l.every, l.burst)}
		l.buckets[key] = e
	}
	e.seen = time.Now()
	l.mu.Unlock()
	r := e.l.Reserve()
	if d := r.Delay(); d > 0 {
		r.Cancel()
		return false, d
	}
	return true, 0
}

// gc drops buckets idle for 10 minutes so memory doesn't grow with clients.
func (l *Limiter) gc() {
	for range time.Tick(time.Minute) {
		l.mu.Lock()
		for k, e := range l.buckets {
			if time.Since(e.seen) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
