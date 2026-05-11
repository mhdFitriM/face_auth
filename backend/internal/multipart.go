package internal

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
)

// BuildBoundaryCommandBody constructs the exact wire format used by the
// HikAccessPushDemo's CommonClass.BoundayProtocolPackage: a leading
// Content-Type + Content-Length header pair, blank line, then multipart parts
// separated by --boundary. The whole result is what gets base64-encoded into
// the command's "data" field.
//
// parts: each MultipartPart has Name, optional Filename, ContentType, and Body.
func BuildBoundaryCommandBody(parts []MultipartPart) []byte {
	const boundary = "boundary"
	var body bytes.Buffer

	for _, p := range parts {
		body.WriteString("--" + boundary + "\r\n")
		if p.Filename != "" {
			fmt.Fprintf(&body, "Content-Disposition: form-data; name=%q; filename=%q\r\n", p.Name, p.Filename)
		} else {
			fmt.Fprintf(&body, "Content-Disposition: form-data; name=%q\r\n", p.Name)
		}
		ct := p.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		fmt.Fprintf(&body, "Content-Type: %s\r\n", ct)
		fmt.Fprintf(&body, "Content-Length: %d\r\n\r\n", len(p.Body))
		body.Write(p.Body)
		body.WriteString("\r\n")
	}
	body.WriteString("--" + boundary + "--")

	final := fmt.Sprintf(
		"Content-Type: multipart/form-data; boundary=%s\r\nContent-Length: %d\r\n\r\n",
		boundary, body.Len(),
	)
	out := make([]byte, 0, len(final)+body.Len())
	out = append(out, []byte(final)...)
	out = append(out, body.Bytes()...)
	return out
}

type MultipartPart struct {
	Name        string
	Filename    string
	ContentType string
	Body        []byte
}

// ParseEmbeddedMultipart parses a payload that begins with HTTP-style headers
// (Content-Type: multipart/...; Content-Length: ...) followed by \r\n\r\n and
// the actual multipart body. Used to decode event payloads from the device.
func ParseEmbeddedMultipart(payload []byte) (parts []MultipartPart, err error) {
	headerEnd := bytes.Index(payload, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return nil, fmt.Errorf("no header terminator found")
	}
	header := string(payload[:headerEnd])

	var contentType string
	for _, line := range strings.Split(header, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			contentType = strings.TrimSpace(line[len("content-type:"):])
			break
		}
	}
	if contentType == "" {
		return nil, fmt.Errorf("no content-type")
	}
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("parse content-type: %w", err)
	}
	if !strings.HasPrefix(mt, "multipart/") {
		return nil, fmt.Errorf("not multipart: %s", mt)
	}
	boundary, ok := params["boundary"]
	if !ok {
		return nil, fmt.Errorf("missing boundary")
	}

	body := payload[headerEnd+4:]
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return parts, err
		}
		buf, err := io.ReadAll(p)
		if err != nil {
			return parts, err
		}
		parts = append(parts, MultipartPart{
			Name:        p.FormName(),
			Filename:    p.FileName(),
			ContentType: p.Header.Get("Content-Type"),
			Body:        buf,
		})
		_ = p.Close()
	}
	return parts, nil
}
