package embed

import (
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
)

// Sparse is a keyword vector: hashed term ids with weights. Qdrant applies
// IDF on its side (the collection's sparse vector uses modifier "idf"), so
// together this behaves like BM25.
type Sparse struct {
	Indices []uint32  `json:"indices"`
	Values  []float32 `json:"values"`
}

// Identifiers such as "LS-3012", "ap-bangkok-1" or "X-Larkspur-Signature"
// are kept whole as well as split into parts, because dense embeddings are
// weakest exactly on these exact-match tokens.
var tokenRe = regexp.MustCompile(`[a-z0-9]+(?:[-_.][a-z0-9]+)*`)

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be by can do does for from has have how i if in is it its
		my of on or our so that the their them then there these this to was we what when where which who will
		with you your yes no not after before than per`) {
		stop[w] = true
	}
}

func Tokens(text string) []string {
	var out []string
	for _, t := range tokenRe.FindAllString(strings.ToLower(text), -1) {
		if !stop[t] {
			out = append(out, t)
		}
		if strings.ContainsAny(t, "-_.") {
			for _, p := range strings.FieldsFunc(t, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
				if !stop[p] && len(p) > 1 {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

const k1 = 1.2

// Document weights saturate term frequency like BM25 (without length
// normalization; chunks are of similar size).
func SparseDoc(text string) Sparse {
	tf := map[uint32]float32{}
	for _, t := range Tokens(text) {
		tf[hash(t)]++
	}
	return build(tf, func(f float32) float32 { return f * (k1 + 1) / (f + k1) })
}

// SparseQuery weights each distinct query term once.
func SparseQuery(text string) Sparse {
	tf := map[uint32]float32{}
	for _, t := range Tokens(text) {
		tf[hash(t)] = 1
	}
	return build(tf, func(f float32) float32 { return f })
}

func build(tf map[uint32]float32, weight func(float32) float32) Sparse {
	s := Sparse{Indices: make([]uint32, 0, len(tf)), Values: make([]float32, 0, len(tf))}
	for idx := range tf {
		s.Indices = append(s.Indices, idx)
	}
	sort.Slice(s.Indices, func(i, j int) bool { return s.Indices[i] < s.Indices[j] })
	for _, idx := range s.Indices {
		s.Values = append(s.Values, weight(tf[idx]))
	}
	return s
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
