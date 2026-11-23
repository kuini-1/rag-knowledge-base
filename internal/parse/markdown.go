package parse

import (
	"regexp"
	"strings"
)

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	mdList    = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
)

// Markdown keeps the source text of each block (tables stay as pipe rows,
// inline formatting is left alone) so offsets map straight back to the file.
func Markdown(src string) (*Document, error) {
	var b builder
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var para []string
	flush := func() {
		if len(para) > 0 {
			b.add(Paragraph, 0, strings.Join(para, " "))
			para = nil
		}
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			flush()
		case strings.HasPrefix(trim, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			b.add(Code, 0, strings.Join(code, "\n"))
		case mdHeading.MatchString(trim):
			flush()
			m := mdHeading.FindStringSubmatch(trim)
			b.add(Heading, len(m[1]), m[2])
		case strings.HasPrefix(trim, "|"):
			flush()
			var rows []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				row := strings.TrimSpace(lines[i])
				if isSeparatorRow(row) {
					continue // |---|---| carries no content
				}
				rows = append(rows, collapseSpaces(row))
			}
			i--
			b.add(Table, 0, strings.Join(rows, "\n"))
		case mdList.MatchString(line):
			flush()
			var items []string
			for ; i < len(lines); i++ {
				l := lines[i]
				if mdList.MatchString(l) {
					items = append(items, strings.TrimSpace(l))
				} else if strings.TrimSpace(l) != "" && strings.HasPrefix(l, "  ") && len(items) > 0 {
					items[len(items)-1] += " " + strings.TrimSpace(l) // continuation line
				} else {
					break
				}
			}
			i--
			b.add(List, 0, strings.Join(items, "\n"))
		default:
			para = append(para, collapseSpaces(trim))
		}
	}
	flush()
	return b.doc("md"), nil
}

func isSeparatorRow(row string) bool {
	return strings.Trim(row, "|-: ") == ""
}

// PlainText splits on blank lines. A short first line in capitals is treated
// as the title.
func PlainText(src string) (*Document, error) {
	var b builder
	paras := regexp.MustCompile(`\n\s*\n`).Split(strings.ReplaceAll(src, "\r\n", "\n"), -1)
	for i, p := range paras {
		p = collapseSpaces(strings.Join(strings.Fields(p), " "))
		if i == 0 && len(p) < 100 && p == strings.ToUpper(p) && strings.ToLower(p) != p {
			b.add(Heading, 1, p)
			continue
		}
		b.add(Paragraph, 0, p)
	}
	return b.doc("txt"), nil
}
