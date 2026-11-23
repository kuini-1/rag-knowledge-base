// Package parse turns uploaded files into a Document: plain text plus the
// structure (headings, paragraphs, tables, lists) that chunking relies on.
//
// Every block records its byte range in Document.Text. Chunks keep those
// ranges, which is how the evaluation decides whether a retrieved chunk
// contains the passage that answers a question, independent of how the
// chunk's text was decorated for embedding.
package parse

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Kind string

const (
	Heading   Kind = "heading"
	Paragraph Kind = "paragraph"
	Table     Kind = "table"
	List      Kind = "list"
	Code      Kind = "code"
)

type Block struct {
	Kind  Kind
	Level int    // heading level, 1 = title
	Text  string // the block's text as it appears in Document.Text
	Start int    // byte offset in Document.Text
	End   int
}

type Document struct {
	Name   string // file name
	Format string // md, txt, pdf, docx
	Title  string
	Text   string
	Blocks []Block
}

// MaxBytes is the largest file Parse accepts.
const MaxBytes = 20 << 20

// Formats lists the supported extensions.
var Formats = []string{".md", ".markdown", ".txt", ".pdf", ".docx"}

// Parse picks a parser from the file extension.
func Parse(name string, data []byte) (*Document, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%s is larger than %d MB", name, MaxBytes>>20)
	}
	var (
		doc *Document
		err error
	)
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".md", ".markdown":
		doc, err = Markdown(string(data))
	case ".txt":
		doc, err = PlainText(string(data))
	case ".pdf":
		doc, err = PDF(data)
	case ".docx":
		doc, err = DOCX(data)
	default:
		return nil, fmt.Errorf("unsupported file type %q (supported: %s)", ext, strings.Join(Formats, ", "))
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	if strings.TrimSpace(doc.Text) == "" {
		return nil, fmt.Errorf("parse %s: no text found (scanned PDFs need OCR, which is not supported)", name)
	}
	doc.Name = name
	if doc.Title == "" {
		doc.Title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	return doc, nil
}

// builder assembles Document.Text from blocks, separated by blank lines.
type builder struct {
	sb     strings.Builder
	blocks []Block
}

func (b *builder) add(kind Kind, level int, text string) {
	text = strings.TrimSpace(strings.ToValidUTF8(text, ""))
	if text == "" {
		return
	}
	if b.sb.Len() > 0 {
		b.sb.WriteString("\n\n")
	}
	start := b.sb.Len()
	b.sb.WriteString(text)
	b.blocks = append(b.blocks, Block{Kind: kind, Level: level, Text: text, Start: start, End: b.sb.Len()})
}

func (b *builder) doc(format string) *Document {
	d := &Document{Format: format, Text: b.sb.String(), Blocks: b.blocks}
	for _, bl := range b.blocks {
		if bl.Kind == Heading && bl.Level == 1 {
			d.Title = bl.Text
			break
		}
	}
	return d
}

// collapseSpaces joins runs of spaces and tabs but keeps line breaks.
func collapseSpaces(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == ' ' {
			space = true
			continue
		}
		if space && sb.Len() > 0 && r != '\n' {
			sb.WriteByte(' ')
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}

// EstimateTokens approximates the token count of English text (about four
// characters per token), which is accurate enough for sizing chunks.
func EstimateTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}
