package anyform

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// File represents a file received from multipart form data.
// It decouples file handling from net/http so it works in any context
// (HTTP handlers, tests, gRPC, CLI tools, etc.).
type File struct {
	// Content holds the raw bytes of the file.
	Content []byte

	// ContentType is the MIME type detected from the multipart Content-Type header
	// or file sniffing. May be empty if unknown.
	ContentType string

	// Filename is the original name of the file as provided by the client.
	Filename string
}

// FileFromHeader extracts a File from a multipart file header.
// It reads the full content into memory and sets ContentType from
// the header's Content-Type field if available.
func FileFromHeader(fh *multipart.FileHeader) (File, error) {
	f, err := fh.Open()
	if err != nil {
		return File{}, fmt.Errorf("opening multipart file: %w", err)
	}
	defer func() { _ = f.Close() }()

	content, err := io.ReadAll(f)
	if err != nil {
		return File{}, fmt.Errorf("reading multipart file: %w", err)
	}

	ct := fh.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(content)
	}

	return File{
		Content:     content,
		ContentType: ct,
		Filename:    fh.Filename,
	}, nil
}

// FilesFromRequest extracts all files for the given field name from an http.Request.
// The request must have been parsed with ParseMultipartForm beforehand.
// Returns nil (not an error) if no files are found for the field.
func FilesFromRequest(r *http.Request, field string) ([]File, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}

	fhs, ok := r.MultipartForm.File[field]
	if !ok || len(fhs) == 0 {
		return nil, nil
	}

	files := make([]File, 0, len(fhs))
	for _, fh := range fhs {
		file, err := FileFromHeader(fh)
		if err != nil {
			return nil, fmt.Errorf("file %q: %w", fh.Filename, err)
		}
		files = append(files, file)
	}

	return files, nil
}
