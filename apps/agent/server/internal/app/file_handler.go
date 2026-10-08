package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/knowledge_index_pipeline"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// maxUploadBytes caps an uploaded document at 50 MB, matching the limit the
// client enforces before it sends the file.
const maxUploadBytes = 50 << 20

// allowedUploadExts are the document types the knowledge base can parse. The
// loader reads plain text, so only text/Markdown extensions are accepted.
var allowedUploadExts = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
}

// handleFileUpload processes file uploads for the knowledge base.
// It saves the file to disk and indexes it into the Milvus vector store
// via the shared IndexFile function (which handles deduplication).
//
// The client-supplied filename is reduced to a single path element and the
// resolved destination is verified to stay inside the configured upload
// directory, so a crafted name cannot write outside the knowledge base.
func (a *App) handleFileUpload(ctx *yukino_http.Context, next func()) {
	file, header, err := ctx.FormFile("file")
	if err != nil {
		ctx.Throw(http.StatusBadRequest, "please upload a file")
		return
	}
	defer file.Close()

	fileName, err := safeUploadName(header.Filename)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, err.Error())
		return
	}

	// Ensure the upload directory exists.
	if err := os.MkdirAll(a.cfg.FileDir, 0o755); err != nil {
		ctx.Throw(http.StatusInternalServerError, "create directory failed: "+err.Error())
		return
	}

	savePath, err := confinedPath(a.cfg.FileDir, fileName)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, err.Error())
		return
	}

	// Stream to disk instead of buffering the whole upload in memory.
	fileSize, err := writeLimited(savePath, file, maxUploadBytes)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, "save file failed: "+err.Error())
		return
	}

	// Index the file into the knowledge base (handles deduplication internally).
	if err := knowledge_index_pipeline.IndexFile(ctx.Request.Context(), a.cfg, savePath); err != nil {
		ctx.Throw(http.StatusInternalServerError, "build knowledge base failed: "+err.Error())
		return
	}

	ctx.Status = http.StatusOK
	ctx.JSON(yukino_http.H{
		"message": "OK",
		"data": yukino_http.H{
			"fileName": fileName,
			"filePath": savePath,
			"fileSize": fileSize,
		},
	})
}

// safeUploadName reduces a client-supplied filename to a single path element
// and rejects values that carry no usable name or an unsupported type.
//
// Both separator styles are normalized first: on Unix a backslash is an
// ordinary character, so filepath.Base would otherwise leave a Windows-style
// traversal sequence such as "..\..\evil.md" intact.
func safeUploadName(raw string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	name := filepath.Base(normalized)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid file name %q", raw)
	}
	if !allowedUploadExts[strings.ToLower(filepath.Ext(name))] {
		return "", fmt.Errorf("unsupported file type %q (want .txt, .md or .markdown)", name)
	}
	return name, nil
}

// confinedPath joins name onto dir and verifies the result stays inside dir.
func confinedPath(dir, name string) (string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve upload directory: %w", err)
	}
	target := filepath.Join(root, name)
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("file name %q escapes the upload directory", name)
	}
	return target, nil
}

// writeLimited streams src into path, refusing to persist more than limit
// bytes. Returns the number of bytes written. A partially written file is
// removed on any failure so a rejected upload leaves no artifact behind.
func writeLimited(path string, src io.Reader, limit int64) (int64, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}

	// Reading one byte past the limit proves the cap was exceeded without
	// buffering the remainder.
	n, err := io.Copy(out, io.LimitReader(src, limit+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return 0, err
	}
	if n > limit {
		os.Remove(path)
		return 0, fmt.Errorf("file exceeds the %d MB limit", limit>>20)
	}
	return n, nil
}
