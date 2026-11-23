"""Build the PDF and DOCX documents of the eval corpus from Markdown sources.

The corpus needs real PDF and Word files to exercise those parsers. This
script renders eval/source/*.md into eval/corpus/ so the binary files can be
regenerated instead of hand-edited.

    python eval/tools/make_formats.py
"""

import re
from pathlib import Path

from docx import Document
from fpdf import FPDF

ROOT = Path(__file__).resolve().parents[1]
SRC, OUT = ROOT / "source", ROOT / "corpus"


def blocks(md: str):
    """Yield (kind, text) for headings, numbered items and paragraphs."""
    for raw in re.split(r"\n\s*\n", md.strip()):
        for line in raw.splitlines() if raw.lstrip().startswith(("1.", "2.", "3.")) else [raw]:
            line = " ".join(line.split())
            if m := re.match(r"^(#{1,3})\s+(.*)", line):
                yield f"h{len(m.group(1))}", m.group(2)
            elif re.match(r"^\d+\.\s", line):
                yield "item", line
            elif line:
                yield "p", line


def to_pdf(src: Path, dst: Path):
    pdf = FPDF(format="A4")
    pdf.set_margins(20, 20, 20)
    pdf.add_page()
    for kind, text in blocks(src.read_text()):
        if kind == "h1":
            pdf.set_font("Helvetica", "B", 16)
            pdf.multi_cell(0, 8, text)
            pdf.ln(3)
        elif kind == "h2":
            pdf.set_font("Helvetica", "B", 12)
            pdf.ln(2)
            pdf.multi_cell(0, 7, text)
            pdf.ln(1)
        else:
            pdf.set_font("Helvetica", "", 10.5)
            pdf.multi_cell(0, 5.5, text)
            pdf.ln(2)
    pdf.output(str(dst))


def to_docx(src: Path, dst: Path):
    doc = Document()
    for kind, text in blocks(src.read_text()):
        if kind.startswith("h"):
            doc.add_heading(text, level=int(kind[1]) - 1 if kind != "h1" else 0)
        elif kind == "item":
            doc.add_paragraph(re.sub(r"^\d+\.\s", "", text), style="List Number")
        else:
            doc.add_paragraph(text)
    doc.save(str(dst))


if __name__ == "__main__":
    to_pdf(SRC / "incident-report-2025-08.md", OUT / "incident-report-2025-08.pdf")
    to_docx(SRC / "data-retention.md", OUT / "data-retention.docx")
    print("wrote", OUT / "incident-report-2025-08.pdf", "and", OUT / "data-retention.docx")
