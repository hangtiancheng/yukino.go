// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/agent/knowledge_index_pipeline"
	"github.com/hangtiancheng/yukino.go/yukino_http"
)

// handleFileUpload processes file uploads for the knowledge base.
// It saves the file to disk and indexes it into the Redis vector store
// via the shared IndexFile function (which handles deduplication).
func (a *App) handleFileUpload(ctx *yukino_http.Context, next func()) {
	file, header, err := ctx.FormFile("file")
	if err != nil {
		ctx.Throw(http.StatusBadRequest, "please upload a file")
		return
	}
	defer file.Close()

	// Ensure the upload directory exists.
	if err := os.MkdirAll(a.cfg.FileDir, 0o755); err != nil {
		ctx.Throw(http.StatusInternalServerError, "create directory failed: "+err.Error())
		return
	}

	fileName := header.Filename
	savePath := filepath.Join(a.cfg.FileDir, fileName)

	// Save the uploaded file.
	data, err := io.ReadAll(file)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, "read file failed: "+err.Error())
		return
	}
	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		ctx.Throw(http.StatusInternalServerError, "save file failed: "+err.Error())
		return
	}

	fileInfo, err := os.Stat(savePath)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, "get file info failed: "+err.Error())
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
			"fileSize": fileInfo.Size(),
		},
	})
}
