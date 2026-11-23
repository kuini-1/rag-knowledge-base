package parse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DOCX reads word/document.xml directly: paragraphs with their style (for
// headings and lists) and tables rendered as pipe-separated rows.
func DOCX(data []byte) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid .docx (zip) file: %w", err)
	}
	var docXML *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docXML = f
			break
		}
	}
	if docXML == nil {
		return nil, errors.New("word/document.xml not found")
	}
	if docXML.UncompressedSize64 > 50<<20 {
		return nil, errors.New("document.xml is too large")
	}
	rc, err := docXML.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var (
		b        builder
		dec      = xml.NewDecoder(io.LimitReader(rc, 50<<20))
		text     strings.Builder
		style    string
		inTable  int
		row      []string
		rows     []string
		listRun  []string
		cellText strings.Builder
	)
	flushList := func() {
		if len(listRun) > 0 {
			b.add(List, 0, strings.Join(listRun, "\n"))
			listRun = nil
		}
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				inTable++
				if inTable == 1 {
					flushList()
					rows = nil
				}
			case "tr":
				row = nil
			case "tc":
				cellText.Reset()
			case "p":
				text.Reset()
				style = ""
			case "pStyle":
				style = attr(t, "val")
			case "tab":
				text.WriteString(" ")
			case "br", "cr":
				text.WriteString(" ")
			case "t":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return nil, err
				}
				text.WriteString(s)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				p := strings.TrimSpace(collapseSpaces(text.String()))
				if inTable > 0 {
					if cellText.Len() > 0 && p != "" {
						cellText.WriteString(" ")
					}
					cellText.WriteString(p)
					continue
				}
				if p == "" {
					continue
				}
				level, isHeading := headingLevel(style)
				switch {
				case isHeading:
					flushList()
					b.add(Heading, level, p)
				case strings.HasPrefix(style, "List"):
					listRun = append(listRun, fmt.Sprintf("%d. %s", len(listRun)+1, p))
				default:
					flushList()
					b.add(Paragraph, 0, p)
				}
			case "tc":
				row = append(row, cellText.String())
			case "tr":
				rows = append(rows, "| "+strings.Join(row, " | ")+" |")
			case "tbl":
				inTable--
				if inTable == 0 {
					b.add(Table, 0, strings.Join(rows, "\n"))
				}
			}
		}
	}
	flushList()
	return b.doc("docx"), nil
}

// headingLevel maps Word styles to levels: Title is 1, Heading1 is 2, and so on.
func headingLevel(style string) (int, bool) {
	s := strings.ToLower(style)
	if s == "title" {
		return 1, true
	}
	if strings.HasPrefix(s, "heading") {
		n, err := strconv.Atoi(strings.TrimPrefix(s, "heading"))
		if err == nil && n >= 1 && n <= 6 {
			return n + 1, true
		}
	}
	return 0, false
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
