package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"strings"
)

// renderDocx produces a minimal but valid .docx containing the ticket.
func renderDocx(doc *exportDoc) ([]byte, error) {
	var body strings.Builder

	subject := exportField(doc.Ticket, "subject")
	display := exportField(doc.Ticket, "display_id")
	if display == "" {
		display = exportField(doc.Ticket, "id")
	}
	writeDocxHeading(&body, fmt.Sprintf("Ticket #%s — %s", display, subject), 1)

	if desc := stripHTML(exportField(doc.Ticket, "description_text")); desc != "" {
		writeDocxPara(&body, desc)
	}
	for _, img := range imagesFor(doc.Images, "ticket") {
		writeDocxImage(&body, img)
	}
	writeDocxAttachmentList(&body, doc.Attachments)

	body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Conversations</w:t></w:r></w:p>`)
	if len(doc.Conversations) == 0 {
		writeDocxPara(&body, "(none)")
	}
	for _, c := range doc.Conversations {
		owner := "conv-" + exportField(c, "id")
		writeDocxConversation(&body, c)
		for _, img := range imagesFor(doc.Images, owner) {
			writeDocxImage(&body, img)
		}
	}

	return buildDocx(body.String(), doc.Images)
}

// imagesFor returns the images owned by owner.
func imagesFor(images []exportImage, owner string) []exportImage {
	var out []exportImage
	for _, img := range images {
		if img.Owner == owner {
			out = append(out, img)
		}
	}
	return out
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

func writeDocxConversation(b *strings.Builder, c map[string]any) {
	dir := "incoming"
	if incoming, ok := c["incoming"].(bool); ok && !incoming {
		dir = "outgoing"
	}
	author := conversationAuthor(c)
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

func writeDocxAttachmentList(b *strings.Builder, atts []exportAttachment) {
	if len(atts) == 0 {
		return
	}
	writeDocxPara(b, "Attachments:")
	for _, a := range atts {
		url := a.URL
		if url == "" {
			url = a.Name
		}
		writeDocxPara(b, fmt.Sprintf("- %s (%s, %d bytes): %s", a.Name, a.ContentType, a.Size, url))
	}
}

// writeDocxImage appends a paragraph containing an inline drawing of the image.
func writeDocxImage(b *strings.Builder, img exportImage) {
	w, h := imageEmu(img.Data)
	id := imageRelID(img.ID)
	b.WriteString(`<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`)
	fmt.Fprintf(b, `<wp:extent cx="%d" cy="%d"/>`, w, h)
	fmt.Fprintf(b, `<wp:docPr id="%d" name="%s"/>`, imageID(img.ID), xmlEscaped(img.Name))
	b.WriteString(`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`)
	fmt.Fprintf(b, `<pic:pic><pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`, imageID(img.ID), xmlEscaped(img.Name))
	fmt.Fprintf(b, `<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`, id)
	fmt.Fprintf(b, `<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`, w, h)
	b.WriteString(`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`)
}

// imageRelID returns a stable relationship id for an image.
func imageRelID(id string) string {
	return "rIdImg" + sanitizeID(id)
}

// imageID returns a stable non-negative numeric id for an image.
func imageID(id string) int {
	h := 0
	for _, r := range sanitizeID(id) {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return h%100000 + 1
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// imageEmu returns the image dimensions in EMUs, scaled to fit roughly 6.5in
// wide, falling back to a default when the image can't be decoded.
func imageEmu(data []byte) (int64, int64) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 900000, 600000
	}
	maxW := 6.5 * 914400 // 6.5in in EMU
	scale := 1.0
	if w := float64(cfg.Width); w > 0 && w > 6.5 {
		scale = maxW / (w * 914400)
	}
	return int64(float64(cfg.Width) * 914400 * scale), int64(float64(cfg.Height) * 914400 * scale)
}

func xmlEscaped(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// imageExt maps a mime type to a file extension for word/media/ entries.
func imageExt(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		exts, _ := mime.ExtensionsByType(mimeType)
		if len(exts) > 0 {
			return strings.TrimPrefix(exts[0], ".")
		}
		return "bin"
	}
}

// buildDocx assembles the OOXML zip.
func buildDocx(documentXML string, images []exportImage) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>`
	for _, img := range images {
		ext := imageExt(img.Mime)
		contentTypes += fmt.Sprintf("\n<Default Extension=\"%s\" ContentType=\"%s\"/>", ext, img.Mime)
	}
	contentTypes += `
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`
	if len(images) > 0 {
		var rb strings.Builder
		rb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
		for _, img := range images {
			fmt.Fprintf(&rb, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`,
				imageRelID(img.ID), imageMediaName(img))
		}
		rb.WriteString(`</Relationships>`)
		rels = rb.String()
	}

	documentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
<w:body>` + documentXML + `
<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>
</w:body></w:document>`

	files := map[string]string{
		"[Content_Types].xml":              contentTypes,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/_rels/document.xml.rels": rels,
		"word/document.xml":            documentXML,
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

	for _, img := range images {
		fw, err := zw.Create("word/media/" + imageMediaName(img))
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(img.Data); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// imageMediaName returns the unique zip part name for an image.
func imageMediaName(img exportImage) string {
	base := sanitizeID(img.Name)
	if base == "" {
		base = sanitizeID(img.ID)
	}
	ext := imageExt(img.Mime)
	if strings.Contains(base, ".") {
		return base
	}
	return base + "." + ext
}

func xmlEscape(b *strings.Builder, s string) {
	if err := xml.EscapeText(b, []byte(s)); err != nil {
		panic("xml.EscapeText: " + err.Error())
	}
}
