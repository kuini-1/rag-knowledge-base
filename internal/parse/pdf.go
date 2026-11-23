package parse

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDF rebuilds lines from positioned text runs, then groups lines into
// paragraphs by vertical spacing and font size. Lines set in a larger font
// than the body text become headings.
func PDF(data []byte) (doc *Document, err error) {
	defer func() {
		// The PDF library panics on some malformed files.
		if r := recover(); r != nil {
			doc, err = nil, errors.New("could not read PDF (malformed or encrypted)")
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	type line struct {
		text string
		size float64
		y    float64
		page int
	}
	var lines []line
	sizeCount := map[float64]int{}

	for pn := 1; pn <= r.NumPage(); pn++ {
		p := r.Page(pn)
		if p.V.IsNull() {
			continue
		}
		texts := p.Content().Text
		// Group runs into rows by their baseline.
		sort.SliceStable(texts, func(i, j int) bool {
			if math.Abs(texts[i].Y-texts[j].Y) > 1 {
				return texts[i].Y > texts[j].Y // top of the page first
			}
			return texts[i].X < texts[j].X
		})
		var cur []pdf.Text
		emit := func() {
			if len(cur) == 0 {
				return
			}
			var sb strings.Builder
			size := 0.0
			for i, t := range cur {
				if i > 0 {
					prev := cur[i-1]
					if gap := t.X - (prev.X + prev.W); gap > prev.FontSize*0.2 && !strings.HasSuffix(sb.String(), " ") {
						sb.WriteByte(' ')
					}
				}
				sb.WriteString(t.S)
				size = math.Max(size, t.FontSize)
			}
			s := strings.TrimSpace(collapseSpaces(sb.String()))
			if s != "" {
				size = math.Round(size*2) / 2
				lines = append(lines, line{text: s, size: size, y: cur[0].Y, page: pn})
				sizeCount[size] += len(s)
			}
			cur = nil
		}
		for _, t := range texts {
			if len(cur) > 0 && math.Abs(t.Y-cur[0].Y) > 1 {
				emit()
			}
			cur = append(cur, t)
		}
		emit()
	}
	if len(lines) == 0 {
		return &Document{Format: "pdf"}, nil
	}

	// The body font size is the one carrying the most text.
	body, most := 0.0, 0
	for s, n := range sizeCount {
		if n > most {
			body, most = s, n
		}
	}
	// Heading levels by size, largest first.
	var bigger []float64
	for s := range sizeCount {
		if s > body*1.1 {
			bigger = append(bigger, s)
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(bigger)))
	level := func(size float64) int {
		for i, s := range bigger {
			if s == size {
				return i + 1
			}
		}
		return 0
	}

	var b builder
	var para []string
	flush := func() {
		if len(para) > 0 {
			b.add(Paragraph, 0, strings.Join(para, " "))
			para = nil
		}
	}
	for i, l := range lines {
		if lv := level(l.size); lv > 0 {
			flush()
			b.add(Heading, lv, l.text)
			continue
		}
		if i > 0 {
			prev := lines[i-1]
			gap := prev.y - l.y
			if prev.page != l.page || gap > l.size*1.9 || level(prev.size) > 0 {
				flush()
			}
		}
		para = append(para, l.text)
	}
	flush()
	return b.doc("pdf"), nil
}
