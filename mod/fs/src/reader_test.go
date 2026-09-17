package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func openHello(t *testing.T) (*os.File, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "hello")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, path
}

func TestReader_ZeroLimitIsEOF(t *testing.T) {
	f, path := openHello(t)
	r := NewReader(f, path, 0, nil)

	n, err := r.Read(make([]byte, 8))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read with limit 0: got %d, %v, want 0, %v", n, err, io.EOF)
	}
}

func TestReader_NegativeLimitReadsWholeFile(t *testing.T) {
	f, path := openHello(t)
	r := NewReader(f, path, -1, nil)

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got, want := string(data), "hello"; got != want {
		t.Fatalf("ReadAll with limit -1: got %q, want %q", got, want)
	}
}

func TestReader_LimitStopsEarly(t *testing.T) {
	f, path := openHello(t)
	r := NewReader(f, path, 3, nil)

	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if got, want := string(buf[:n]), "hel"; got != want {
		t.Fatalf("first Read with limit 3: got %q, want %q", got, want)
	}

	n, err = r.Read(buf)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("Read past limit: got %d, %v, want 0, %v", n, err, io.EOF)
	}
}
