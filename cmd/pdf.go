package cmd

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/go-pdf/fpdf"
)

//go:embed assets/DejaVuSansCondensed.ttf
var dejaVuRegular []byte

//go:embed assets/DejaVuSansCondensed-Bold.ttf
var dejaVuBold []byte

func renderPDF(doc *exportDoc) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddUTF8FontFromBytes("DejaVu", "", dejaVuRegular)
	pdf.AddUTF8FontFromBytes("DejaVu", "B", dejaVuBold)
	pdf.SetFont("DejaVu", "", 11)

	pdf.AddPage()

	subject := exportField(doc.Ticket, "subject")
	display := exportField(doc.Ticket, "display_id")
	if display == "" {
		display = exportField(doc.Ticket, "id")
	}
	pdf.SetFont("DejaVu", "B", 16)
	pdf.MultiCell(0, 8, fmt.Sprintf("Ticket #%s — %s", display, subject), "", "L", false)
	pdf.Ln(4)

	if desc := stripHTML(exportField(doc.Ticket, "description_text")); desc != "" {
		pdf.SetFont("DejaVu", "", 11)
		pdf.MultiCell(0, 6, desc, "", "L", false)
		pdf.Ln(4)
	}

	pdf.SetFont("DejaVu", "B", 13)
	pdf.Cell(0, 8, "Conversations")
	pdf.Ln(8)

	pdf.SetFont("DejaVu", "", 11)
	if len(doc.Conversations) == 0 {
		pdf.Cell(0, 7, "(none)")
		pdf.Ln(7)
	}
	for _, c := range doc.Conversations {
		writePDFConversation(pdf, c)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePDFConversation(pdf *fpdf.Fpdf, c map[string]any) {
	dir := "incoming"
	if incoming, ok := c["incoming"].(bool); ok && !incoming {
		dir = "outgoing"
	}
	author := conversationAuthor(c)
	stamp := exportField(c, "created_at")
	bodyText := stripHTML(exportField(c, "body_text"))

	pdf.SetFont("DejaVu", "B", 11)
	pdf.MultiCell(0, 6, fmt.Sprintf("%s (%s, %s)", author, dir, stamp), "", "L", false)
	pdf.SetFont("DejaVu", "", 11)
	if bodyText != "" {
		pdf.MultiCell(0, 6, bodyText, "", "L", false)
	} else {
		pdf.MultiCell(0, 6, "(no body)", "", "L", false)
	}
	pdf.Ln(3)
}
