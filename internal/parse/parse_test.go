package parse

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func docx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `</w:body></w:document>`))
	zw.Close()
	return buf.Bytes()
}

func p(style, text string) string {
	s := ""
	if style != "" {
		s = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + s + `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func TestDOCX(t *testing.T) {
	cell := func(s string) string { return `<w:tc>` + p("", s) + `</w:tc>` }
	data := docx(t, p("Title", "Retention Policy")+
		p("Heading1", "Logs")+
		p("", "Request logs are kept for 400 days.")+
		p("ListNumber", "Export them first.")+
		p("ListNumber", "Then delete.")+
		`<w:tbl><w:tr>`+cell("Type")+cell("Days")+`</w:tr><w:tr>`+cell("Audit")+cell("400")+`</w:tr></w:tbl>`)

	d, err := Parse("policy.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Retention Policy" {
		t.Errorf("title = %q", d.Title)
	}
	want := []struct {
		kind  Kind
		level int
		text  string
	}{
		{Heading, 1, "Retention Policy"},
		{Heading, 2, "Logs"},
		{Paragraph, 0, "Request logs are kept for 400 days."},
		{List, 0, "1. Export them first.\n2. Then delete."},
		{Table, 0, "| Type | Days |\n| Audit | 400 |"},
	}
	if len(d.Blocks) != len(want) {
		t.Fatalf("got %d blocks: %+v", len(d.Blocks), d.Blocks)
	}
	for i, w := range want {
		b := d.Blocks[i]
		if b.Kind != w.kind || b.Level != w.level || b.Text != w.text {
			t.Errorf("block %d = %s/%d %q, want %s/%d %q", i, b.Kind, b.Level, b.Text, w.kind, w.level, w.text)
		}
		if d.Text[b.Start:b.End] != b.Text {
			t.Errorf("block %d offsets don't match the text", i)
		}
	}
}

func TestMarkdownTablesAndLists(t *testing.T) {
	d, err := Markdown("# T\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- one\n- two\n  continued\n\nText *here*.\n")
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, b := range d.Blocks {
		kinds = append(kinds, string(b.Kind))
	}
	if strings.Join(kinds, ",") != "heading,table,list,paragraph" {
		t.Fatalf("blocks = %v", kinds)
	}
	if d.Blocks[1].Text != "| a | b |\n| 1 | 2 |" {
		t.Errorf("separator row not dropped: %q", d.Blocks[1].Text)
	}
	if d.Blocks[2].Text != "- one\n- two continued" {
		t.Errorf("list continuation: %q", d.Blocks[2].Text)
	}
}

func TestRejects(t *testing.T) {
	if _, err := Parse("x.exe", []byte("MZ")); err == nil {
		t.Error("unsupported extension accepted")
	}
	if _, err := Parse("empty.md", []byte("   \n")); err == nil {
		t.Error("empty document accepted")
	}
	if _, err := Parse("bad.pdf", []byte("not a pdf")); err == nil {
		t.Error("garbage PDF accepted")
	}
	if _, err := Parse("bad.docx", []byte("not a zip")); err == nil {
		t.Error("garbage DOCX accepted")
	}
}
