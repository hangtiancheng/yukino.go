package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeUploadName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "guide.md", want: "guide.md"},
		{in: "notes.txt", want: "notes.txt"},
		{in: "doc.markdown", want: "doc.markdown"},
		{in: "UPPER.MD", want: "UPPER.MD"},
		{in: "../../etc/passwd", want: "passwd", wantErr: true},
		{in: "../../etc/passwd.md", want: "passwd.md"},
		{in: `..\..\evil.md`, want: "evil.md"},
		{in: "/abs/dir/file.md", want: "file.md"},
		{in: "..", wantErr: true},
		{in: ".", wantErr: true},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "malware.exe", wantErr: true},
		{in: "image.png", wantErr: true},
		{in: "noext", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := safeUploadName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("safeUploadName(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeUploadName(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("safeUploadName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestConfinedPath(t *testing.T) {
	dir := t.TempDir()

	got, err := confinedPath(dir, "ok.md")
	if err != nil {
		t.Fatalf("confinedPath(ok.md): %v", err)
	}
	if want := filepath.Join(dir, "ok.md"); got != want {
		t.Errorf("confinedPath = %q, want %q", got, want)
	}

	if _, err := confinedPath(dir, ".."); err == nil {
		t.Error("confinedPath(dir, \"..\") = nil error, want escape rejection")
	}
}

func TestWriteLimited(t *testing.T) {
	dir := t.TempDir()

	t.Run("under limit", func(t *testing.T) {
		path := filepath.Join(dir, "small.md")
		n, err := writeLimited(path, strings.NewReader("hello"), 10)
		if err != nil {
			t.Fatalf("writeLimited: %v", err)
		}
		if n != 5 {
			t.Errorf("n = %d, want 5", n)
		}
		b, _ := os.ReadFile(path)
		if string(b) != "hello" {
			t.Errorf("content = %q, want %q", b, "hello")
		}
	})

	t.Run("over limit removes file", func(t *testing.T) {
		path := filepath.Join(dir, "big.md")
		if _, err := writeLimited(path, strings.NewReader(strings.Repeat("x", 100)), 10); err == nil {
			t.Fatal("writeLimited = nil error, want limit error")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("oversized file still present after rejection (stat err = %v)", err)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		path := filepath.Join(dir, "exact.md")
		n, err := writeLimited(path, strings.NewReader("12345"), 5)
		if err != nil {
			t.Fatalf("writeLimited at exact limit: %v", err)
		}
		if n != 5 {
			t.Errorf("n = %d, want 5", n)
		}
	})
}
