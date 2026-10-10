package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/knowledge_index_pipeline"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

const maxUploadBytes = 50 << 20

var allowedUploadExts = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
}

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

	if err := os.MkdirAll(a.cfg.FileDir, 0o755); err != nil {
		ctx.Throw(http.StatusInternalServerError, "create directory failed: "+err.Error())
		return
	}

	savePath, err := confinedPath(a.cfg.FileDir, fileName)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, err.Error())
		return
	}

	fileSize, err := writeLimited(savePath, file, maxUploadBytes)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, "save file failed: "+err.Error())
		return
	}

	// Index with a context detached from the request: the file is already
	// saved on disk, and IndexFile deletes stale chunks before re-indexing.
	// Cancelling mid-way on client disconnect would leave the knowledge base
	// with the old chunks removed and the new ones only partially inserted.
	if err := knowledge_index_pipeline.IndexFile(context.WithoutCancel(ctx.Request.Context()), a.cfg, savePath); err != nil {
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

func writeLimited(path string, src io.Reader, limit int64) (int64, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}

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
