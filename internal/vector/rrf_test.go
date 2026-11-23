package vector

import "testing"

func ids(hs []Hit) string {
	s := ""
	for _, h := range hs {
		s += h.ID
	}
	return s
}

func TestFuseRRF(t *testing.T) {
	dense := []Hit{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	sparse := []Hit{{ID: "c"}, {ID: "d"}, {ID: "a"}}
	// a: 1/61 + 1/63, c: 1/63 + 1/61 (tie, both best rank 1 -> id order), b: 1/62, d: 1/62 (tie -> id order)
	if got := ids(FuseRRF(dense, sparse, 4)); got != "acbd" {
		t.Errorf("order = %s, want acbd", got)
	}
	if got := ids(FuseRRF(dense, sparse, 2)); got != "ac" {
		t.Errorf("limit: %s", got)
	}
	// Same input, same output, every time.
	for i := 0; i < 50; i++ {
		if ids(FuseRRF(sparse, dense, 4)) != "acbd" {
			t.Fatal("fusion is not deterministic")
		}
	}
}
