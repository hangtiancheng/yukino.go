package storage

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/cloud_storage/pkg/asynclog"
	"github.com/hangtiancheng/yukino.go/apps/cloud_storage/pkg/bundle"
)

// indexTemplate is the embedded web UI. Embedding it removes the original's
// dependency on an index.html file being present in the working directory.
//
//go:embed web/index.html
var indexTemplate string

// Service is the HTTP front end of the cloud storage system.
type Service struct {
	cfg  *Config
	data *Manager
	log  *asynclog.Logger
	tmpl *template.Template

	objects sync.RWMutex // protects publishing files and their metadata
}

// NewService builds the service. The template is parsed once at construction so
// a malformed template fails fast instead of at request time.
func NewService(cfg *Config, data *Manager, log *asynclog.Logger) (*Service, error) {
	tmpl, err := template.New("index").Parse(indexTemplate)
	if err != nil {
		return nil, fmt.Errorf("storage: parse index template: %w", err)
	}
	return &Service{cfg: cfg, data: data, log: log, tmpl: tmpl}, nil
}

// Handler returns the yukino_http application with every route registered.
func (s *Service) Handler() *yukino.Application {
	app := yukino.New()
	app.Use(yukino.Recovery())
	app.Use(s.accessLog())
	app.Get("/", s.handleList)
	app.Post("/upload", s.handleUpload)
	prefix := strings.TrimSuffix(s.cfg.DownloadPrefix, "/")
	app.Get(prefix+"/*name", s.handleDownload)
	// GET does not implicitly cover HEAD, so register it explicitly;
	// http.ServeContent answers HEAD requests without a body.
	app.Head(prefix+"/*name", s.handleDownload)
	return app
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts the
// server down gracefully. Unlike the original, which looped forever with no way
// to stop, this integrates with the process signal handling.
func (s *Service) Run(ctx context.Context) error {
	addr := s.cfg.Addr()
	// Bind synchronously so a failure (for example a port already in use) is
	// reported before anything claims the service is listening, and so the
	// logged address is the real one even when port 0 was configured.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("storage: listen on %s: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       60 * time.Second,
		// ReadTimeout and WriteTimeout are intentionally left unset: uploads and
		// downloads are streamed and must not be cut off by a fixed deadline.
		// Uploads are bounded by MaxBytesReader instead.
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Infof("storage service listening on %s", ln.Addr())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		s.log.Infof("storage service shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// accessLog records one line per request through the async logger, replacing
// the framework's standard-library logger so all output flows through the same
// asynchronous pipeline.
func (s *Service) accessLog() yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		start := time.Now()
		next()
		s.log.Infof("%s %s -> %d (%s)", ctx.Method, ctx.Request.URL.Path, ctx.Status, time.Since(start))
	}
}

// pageData is the template view model for the file list page.
type pageData struct {
	Files []fileView
}

// fileView is the per-file template view model.
type fileView struct {
	Name string
	URL  string
	Type string
	Size string
	Time string
}

func newFileView(info *StorageInfo) fileView {
	tier := "low"
	if info.Deep {
		tier = "deep"
	}
	return fileView{
		Name: info.Name,
		URL:  info.URL,
		Type: tier,
		Size: formatSize(info.Size),
		Time: time.Unix(info.ModTime, 0).Format("2006-01-02 15:04:05"),
	}
}

// formatSize renders a byte count using binary units.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// handleList renders the web UI with the current file list.
func (s *Service) handleList(ctx *yukino.Context, next func()) {
	records := s.data.All()
	files := make([]fileView, 0, len(records))
	for _, r := range records {
		files = append(files, newFileView(r))
	}
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, pageData{Files: files}); err != nil {
		s.log.Errorf("render index template: %v", err)
		ctx.Throw(http.StatusInternalServerError, "failed to render page")
		return
	}
	ctx.Set("Content-Type", "text/html; charset=utf-8")
	ctx.Data(buf.Bytes())
}

// handleUpload streams an uploaded file into the selected storage tier.
//
// Compared with the original this validates the file name header, sanitises the
// name against path traversal, and streams the body to disk under a size limit
// instead of buffering the whole request in memory.
func (s *Service) handleUpload(ctx *yukino.Context, next func()) {
	name, err := s.decodeFileName(ctx.Get("FileName"))
	if err != nil {
		ctx.Throw(http.StatusBadRequest, err.Error())
		return
	}

	storageType := strings.ToLower(strings.TrimSpace(ctx.Get("StorageType")))
	var dir string
	var deep bool
	switch storageType {
	case "low":
		dir, deep = s.cfg.LowStorageDir, false
	case "deep":
		dir, deep = s.cfg.DeepStorageDir, true
	default:
		ctx.Throw(http.StatusBadRequest, "illegal storage type")
		return
	}
	if err := EnsureDir(dir); err != nil {
		s.log.Errorf("create storage dir %s: %v", dir, err)
		ctx.Throw(http.StatusInternalServerError, "cannot prepare storage directory")
		return
	}
	dest, err := SafeJoin(dir, name)
	if err != nil {
		ctx.Throw(http.StatusBadRequest, err.Error())
		return
	}

	body := http.MaxBytesReader(ctx.Writer, ctx.Request.Body, s.cfg.MaxUploadBytes)
	defer body.Close()

	staged, err := s.stageObject(filepath.Dir(dest), body, deep)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			ctx.Throw(http.StatusRequestEntityTooLarge, "upload exceeds the configured size limit")
			return
		}
		s.log.Errorf("store %s: %v", dest, err)
		ctx.Throw(http.StatusInternalServerError, "failed to store object")
		return
	}

	defer os.Remove(staged)
	info, err := s.publishObject(staged, dest, deep)
	if err != nil {
		s.log.Errorf("publish %s: %v", dest, err)
		ctx.Throw(http.StatusInternalServerError, "failed to publish object")
		return
	}
	s.log.Infof("upload stored %s (%d bytes, %s storage)", info.StoragePath, info.Size, storageType)
	ctx.JSON(yukino.H{"message": "success", "data": yukino.H{"url": info.URL, "size": info.Size}})
}

// decodeFileName base64-decodes and sanitises the FileName request header.
func (s *Service) decodeFileName(header string) (string, error) {
	if header == "" {
		return "", errors.New("missing FileName header")
	}
	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		// Tolerate the URL-safe alphabet some clients emit.
		if raw, err = base64.URLEncoding.DecodeString(header); err != nil {
			return "", errors.New("FileName header is not valid base64")
		}
	}
	return SafeFileName(string(raw))
}

// stageObject writes the complete upload beside its destination before publication.
func (s *Service) stageObject(dir string, src io.Reader, deep bool) (string, error) {
	f, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
			_ = os.Remove(name)
		}
	}()

	if deep {
		bw, err := bundle.NewWriter(f, s.cfg.BundleCodec(), bundle.DefaultLevel)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(bw, src)
		if err := errors.Join(copyErr, bw.Close()); err != nil {
			return "", err
		}
	} else if _, err := io.Copy(f, src); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	keep = true
	return name, nil
}

func (s *Service) publishObject(staged, dest string, deep bool) (*StorageInfo, error) {
	s.objects.Lock()
	defer s.objects.Unlock()

	// Keep the previous inode until the new metadata has been persisted.
	backup := staged + ".previous"
	hasPrevious := true
	if err := os.Link(dest, backup); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		hasPrevious = false
	} else {
		defer func() {
			if backup != "" {
				_ = os.Remove(backup)
			}
		}()
	}

	if err := os.Rename(staged, dest); err != nil {
		return nil, err
	}
	info, err := NewStorageInfo(dest, s.cfg.DownloadPrefix, deep)
	if err == nil {
		info.Version = filepath.Base(staged)
		err = s.data.Insert(info)
	}
	if err == nil {
		return info, nil
	}
	if hasPrevious {
		if restoreErr := os.Rename(backup, dest); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore object from %s: %w", backup, restoreErr))
			backup = "" // retain the previous object for recovery
		}
	} else {
		err = errors.Join(err, os.Remove(dest))
	}
	return nil, err
}

// handleDownload serves a stored object. Deep-storage objects are decompressed
// into a unique temporary file first so that http.ServeContent can honour range
// and conditional requests; the original decompressed into the shared low
// storage directory using the bundle's base name, which collided with real files
// and raced between concurrent downloads.
func (s *Service) handleDownload(ctx *yukino.Context, next func()) {
	resourcePath := ctx.Request.URL.Path
	s.objects.RLock()
	info, ok := s.data.GetByURL(resourcePath)
	if !ok {
		s.objects.RUnlock()
		ctx.Throw(http.StatusNotFound, "file not found")
		return
	}
	stored, err := os.Open(info.StoragePath)
	s.objects.RUnlock()
	if err != nil {
		s.log.Errorf("open %s: %v", info.StoragePath, err)
		if os.IsNotExist(err) {
			ctx.Throw(http.StatusNotFound, "stored object is missing")
		} else {
			ctx.Throw(http.StatusInternalServerError, "failed to open object")
		}
		return
	}
	defer stored.Close()
	st, err := stored.Stat()
	if err != nil {
		s.log.Errorf("stat %s: %v", info.StoragePath, err)
		ctx.Throw(http.StatusInternalServerError, "failed to stat object")
		return
	}
	f := stored
	if info.Deep {
		f, err = s.materialise(stored)
		if err != nil {
			s.log.Errorf("decompress %s: %v", info.StoragePath, err)
			ctx.Throw(http.StatusInternalServerError, "failed to prepare download")
			return
		}
		defer os.Remove(f.Name())
		defer f.Close()
	}

	ctx.Set("Content-Type", "application/octet-stream")
	ctx.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, escapeQuotes(info.Name)))
	ctx.Set("ETag", info.ETag())
	ctx.Set("Accept-Ranges", "bytes")
	// Take over the response and let http.ServeContent implement conditional,
	// range and HEAD semantics correctly; the original hand-rolled this and
	// ignored the Range header entirely.
	ctx.TakeOver()
	rec := &statusRecorder{ResponseWriter: ctx.Writer, status: &ctx.Status}
	http.ServeContent(rec, ctx.Request, info.Name, st.ModTime(), f)
	s.log.Infof("download served %s (%s)", info.URL, resourcePath)
}

// materialise decodes a bundle into a temporary file and rewinds it.
func (s *Service) materialise(src io.Reader) (*os.File, error) {
	tmpDir := s.cfg.TempDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	if err := EnsureDir(tmpDir); err != nil {
		return nil, err
	}
	br, err := bundle.NewReader(src)
	if err != nil {
		return nil, err
	}
	defer br.Close()
	tmp, err := os.CreateTemp(tmpDir, "yukino-dl-*")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, br); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	keep = true
	return tmp, nil
}

// escapeQuotes escapes double quotes for use in a quoted Content-Disposition
// filename. Names are already sanitised on upload, so this is defensive.
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// statusRecorder mirrors the final wire status into ctx.Status so the access
// log stays accurate when the response is taken over by http.ServeContent.
type statusRecorder struct {
	http.ResponseWriter
	status *int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(code int) {
	if !w.wrote {
		*w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		*w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// ReadFrom delegates to the underlying writer when it supports io.ReaderFrom,
// so large downloads can use the kernel's zero-copy path instead of buffering
// every byte through this wrapper.
func (w *statusRecorder) ReadFrom(r io.Reader) (int64, error) {
	if !w.wrote {
		*w.status = http.StatusOK
		w.wrote = true
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	// The anonymous struct hides both wrappers from io.Copy so it cannot
	// recurse back into ReadFrom.
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, r)
}

// Flush exposes the underlying writer's http.Flusher support, if any.
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
