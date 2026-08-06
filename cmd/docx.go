package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// renderDocx produces a minimal but valid .docx containing the ticket.
func renderDocx(doc *exportDoc, includeTags bool) ([]byte, error) {
	var body strings.Builder
	body.WriteString(`<w:body>`)

	subject := exportField(doc.Ticket, "subject")
	display := exportField(doc.Ticket, "display_id")
	if display == "" {
		display = exportField(doc.Ticket, "id")
	}
	writeDocxHeading(&body, fmt.Sprintf("Ticket #%s — %s", display, subject), 1)

	body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Summary</w:t></w:r></w:p>`)
	writeDocxSummary(&body, doc.Ticket, includeTags)

	body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Conversations</w:t></w:r></w:p>`)
	if len(doc.Conversations) == 0 {
		writeDocxPara(&body, "(none)")
	}
	for _, c := range doc.Conversations {
		writeDocxConversation(&body, c)
	}

	body.WriteString(`</w:body>`)
	return buildDocx(body.String())
}

func writeDocxHeading(b *strings.Builder, text string, level int) {
	style := "Heading1"
	if level == 2 {
		style = "Heading2"
	}
	b.WriteString(`<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>`)
	xmlEscape(b, text)
	b.WriteString(`</w:t></w:r></w:p>`)
}

func writeDocxPara(b *strings.Builder, text string) {
	b.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
	xmlEscape(b, text)
	b.WriteString(`</w:t></w:r></w:p>`)
}

func writeDocxSummary(b *strings.Builder, t map[string]any, includeTags bool) {
	rows := []struct{ label, key string }{
		{"Status", "status"}, {"Priority", "priority"}, {"Urgency", "urgency"},
		{"Impact", "impact"}, {"Requester", "requester_name"}, {"Requester ID", "requester_id"},
		{"Responder", "responder_name"}, {"Responder ID", "responder_id"},
		{"Group", "group_name"}, {"Department", "department_name"},
		{"Created", "created_at"}, {"Updated", "updated_at"}, {"Due", "due_by"},
	}
	for _, r := range rows {
		if v := exportField(t, r.key); v != "" {
			b.WriteString(`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="3600"/></w:pPr><w:r><w:t>`)
			xmlEscape(b, r.label+":")
			b.WriteString(`</w:t></w:r><w:r><w:tab/><w:t xml:space="preserve">`)
			xmlEscape(b, v)
			b.WriteString(`</w:t></w:r></w:p>`)
		}
	}
	if desc := stripHTML(exportField(t, "description_text")); desc != "" {
		writeDocxPara(b, "Description: "+desc)
	}
	if includeTags {
		if tags := exportField(t, "tags"); tags != "" {
			writeDocxPara(b, "Tags: "+tags)
		}
	}
	if cf := exportField(t, "custom_fields"); cf != "" && cf != "null" {
		writeDocxPara(b, "Custom fields: "+cf)
	}
}

func writeDocxConversation(b *strings.Builder, c map[string]any) {
	dir := "incoming"
	if incoming, ok := c["incoming"].(bool); ok && !incoming {
		dir = "outgoing"
	}
	author := exportField(c, "user_id")
	if u, ok := c["user"].(map[string]any); ok {
		if name := exportField(u, "name"); name != "" {
			author = name
		}
	}
	stamp := exportField(c, "created_at")
	bodyText := stripHTML(exportField(c, "body_text"))

	b.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading3"/></w:pPr><w:r><w:t>`)
	xmlEscape(b, fmt.Sprintf("%s (%s, %s)", author, dir, stamp))
	b.WriteString(`</w:t></w:r></w:p>`)
	if bodyText != "" {
		writeDocxPara(b, bodyText)
	} else {
		writeDocxPara(b, "(no body)")
	}
}

// buildDocx assembles the minimal OOXML zip.
func buildDocx(documentXML string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>` + documentXML + `
</w:body></w:document>`,
	}

	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func xmlEscape(b *strings.Builder, s string) {
	_ = xml.EscapeText(b, []byte(s))
}
