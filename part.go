package enmime

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"github.com/sloonz/go-qprintable"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// MIMEPart is the primary interface enmine clients will use.  Each MIMEPart represents
// a node in the MIME multipart tree.  The Content-Type, Disposition and File Name are
// parsed out of the header for easier access.
//
// TODO Content should probably be a reader so that it does not need to be stored in
// memory.
type MIMEPart interface {
	Parent() MIMEPart             // Parent of this part (can be nil)
	FirstChild() MIMEPart         // First (top most) child of this part
	NextSibling() MIMEPart        // Next sibling of this part
	Header() textproto.MIMEHeader // Header as parsed by textproto package
	ContentType() string          // Content-Type header without parameters
	Disposition() string          // Content-Disposition header without parameters
	FileName() string             // File Name from disposition or type header
	Content() []byte              // Decoded content of this part (can be empty)
}

// memMIMEPart is an in-memory implementation of the MIMEPart interface.  It will likely
// choke on huge attachments.
type memMIMEPart struct {
	parent      MIMEPart
	firstChild  MIMEPart
	nextSibling MIMEPart
	header      textproto.MIMEHeader
	contentType string
	disposition string
	fileName    string
	content     []byte
}

// NewMIMEPart creates a new memMIMEPart object.  It does not update the parents FirstChild
// attribute.
func NewMIMEPart(parent MIMEPart, contentType string) *memMIMEPart {
	return &memMIMEPart{parent: parent, contentType: contentType}
}

// Parent of this part (can be nil)
func (p *memMIMEPart) Parent() MIMEPart {
	return p.parent
}

// First (top most) child of this part
func (p *memMIMEPart) FirstChild() MIMEPart {
	return p.firstChild
}

// Next sibling of this part
func (p *memMIMEPart) NextSibling() MIMEPart {
	return p.nextSibling
}

// Header as parsed by textproto package
func (p *memMIMEPart) Header() textproto.MIMEHeader {
	return p.header
}

// Content-Type header without parameters
func (p *memMIMEPart) ContentType() string {
	return p.contentType
}

// Content-Disposition header without parameters
func (p *memMIMEPart) Disposition() string {
	return p.disposition
}

// File Name from disposition or type header
func (p *memMIMEPart) FileName() string {
	return p.fileName
}

// Decoded content of this part (can be empty)
func (p *memMIMEPart) Content() []byte {
	return p.content
}

// ParseMIME reads a MIME document from the provided reader and parses it into
// tree of MIMEPart objects.
func ParseMIME(reader *bufio.Reader) (MIMEPart, error) {
	tr := textproto.NewReader(reader)
	header, err := tr.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	mediatype, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	root := &memMIMEPart{header: header, contentType: mediatype}

	if strings.HasPrefix(mediatype, "multipart/") {
		boundary := params["boundary"]
		err = parseParts(root, reader, boundary)
		if err != nil {
			return nil, err
		}
	} else {
		// Content is text or data, decode it
		content, err := decodeSection(header.Get("Content-Transfer-Encoding"), reader)
		if err != nil {
			return nil, err
		}
		root.content = content
	}

	return root, nil
}

// parseParts recursively parses a mime multipart document.
func parseParts(parent *memMIMEPart, reader io.Reader, boundary string) error {
	var prevSibling *memMIMEPart

	// Loop over MIME parts
	mr := multipart.NewReader(reader, boundary)
	for {
		// mrp is go's build in mime-part
		mrp, err := mr.NextPart()
		if err != nil {
			if err == io.EOF {
				// This is a clean end-of-message signal
				break
			}
			return err
		}
		mediatype, mparams, err := mime.ParseMediaType(mrp.Header.Get("Content-Type"))
		if err != nil {
			return err
		}

		// Insert ourselves into tree, p is go-mime's mime-part
		p := NewMIMEPart(parent, mediatype)
		if prevSibling != nil {
			prevSibling.nextSibling = p
		} else {
			parent.firstChild = p
		}
		prevSibling = p

		// Figure out our disposition, filename
		disposition, dparams, err := mime.ParseMediaType(mrp.Header.Get("Content-Disposition"))
		if err == nil {
			// Disposition is optional
			p.disposition = disposition
			p.fileName = dparams["filename"]
		}
		if p.fileName == "" && mparams["name"] != "" {
			p.fileName = mparams["name"]
		}

		boundary := mparams["boundary"]
		if boundary != "" {
			// Content is another multipart
			err = parseParts(p, mrp, boundary)
			if err != nil {
				return err
			}
		} else {
			// Content is text or data, decode it
			data, err := decodeSection(mrp.Header.Get("Content-Transfer-Encoding"), mrp)
			if err != nil {
				return err
			}
			p.content = data
		}
	}

	return nil
}

// decodeSection attempts to decode the data from reader using the algorithm listed in
// the Content-Transfer-Encoding header, returning the raw data if it does not known
// the encoding type.
func decodeSection(encoding string, reader io.Reader) ([]byte, error) {
	// Default is to just read input into bytes
	decoder := reader

	switch strings.ToLower(encoding) {
	case "quoted-printable":
		decoder = qprintable.NewDecoder(qprintable.WindowsTextEncoding, reader)
	case "base64":
		cleaner := NewBase64Cleaner(reader)
		decoder = base64.NewDecoder(base64.StdEncoding, cleaner)
	}

	// Read bytes into buffer
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(decoder)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
