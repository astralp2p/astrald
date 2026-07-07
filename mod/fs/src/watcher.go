package fs

import (
	"errors"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultWriteTimeout is the quiet-period after the last write event before OnWriteDone is fired.
const DefaultWriteTimeout = 3 * time.Second

// Watcher wraps fsnotify with per-file write debouncing: OnWriteDone fires only after no further
// write events arrive within WriteTimeout, preventing partial-file processing.
// All callback fields are optional; a nil callback is silently skipped.
type Watcher struct {
	OnWrite       func(string)
	OnWriteDone   func(string) // called WriteTimeout after last write
	OnFileCreated func(string)
	OnDirCreated  func(string)
	OnRenamed     func(string) // path was renamed to something else
	OnRemoved     func(string)
	OnChmod       func(string)
	OnError       func(error)
	WriteTimeout  time.Duration

	watcher  *fsnotify.Watcher
	timeouts map[string]*time.Time
	mu       sync.Mutex
}

// NewWatcher creates a Watcher and starts its background event-dispatch goroutine.
func NewWatcher() (*Watcher, error) {
	var err error
	var w = &Watcher{
		WriteTimeout: DefaultWriteTimeout,
		timeouts:     map[string]*time.Time{},
	}

	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	go w.worker()

	return w, nil
}

// Add adds a path to the watcher
func (w *Watcher) Add(path string, tree bool) (added []string, err error) {
	if slices.Contains(w.watcher.WatchList(), path) {
		return nil, errors.New("already added")
	}

	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, errors.New("not a directory")
	}

	err = w.watcher.Add(path)
	if err != nil {
		return nil, err
	}
	added = append(added, path)

	if !tree {
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		a, _ := w.Add(filepath.Join(path, entry.Name()), tree)
		added = append(added, a...)
	}

	return nil, nil
}

// Remove removes a path from the watcher
func (w *Watcher) Remove(path string, tree bool) error {
	err := w.watcher.Remove(path)
	if err != nil {
		return err
	}

	if !tree {
		return nil
	}

	for _, p := range w.watcher.WatchList() {
		if strings.HasPrefix(p, path+"/") {
			w.Remove(p, true)
		}
	}

	return nil
}

// Paths returns all paths being watched for events
func (w *Watcher) Paths() []string {
	return w.watcher.WatchList()
}

// Close stops the watcher and releases resources
func (w *Watcher) Close() error {
	return w.watcher.Close()
}

func (w *Watcher) onWrite(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	t, found := w.timeouts[path]
	if found {
		*t = time.Now().Add(w.WriteTimeout)
		return
	}

	w.timeouts[path] = &time.Time{}
	*w.timeouts[path] = time.Now().Add(w.WriteTimeout)

	sig.At(w.timeouts[path], &w.mu, func() {
		w.mu.Lock()
		delete(w.timeouts, path)
		w.mu.Unlock()

		if w.OnWriteDone != nil {
			w.OnWriteDone(path)
		}
	})
}

func (w *Watcher) onRemoved(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	t, found := w.timeouts[path]
	if !found {
		return
	}

	*t = time.Time{}
}

func (w *Watcher) onRenamed(path string) {
	w.onRemoved(path)
}

func (w *Watcher) worker() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			switch {
			case event.Op.Has(fsnotify.Write):
				w.onWrite(event.Name)
				if w.OnWrite != nil {
					w.OnWrite(event.Name)
				}

			case event.Op.Has(fsnotify.Rename):
				w.onRenamed(event.Name)
				if w.OnRenamed != nil {
					w.OnRenamed(event.Name)
				}

			case event.Op.Has(fsnotify.Create):
				info, err := os.Stat(event.Name)
				switch {
				case err != nil:
				case info.Mode().IsRegular():
					if w.OnFileCreated != nil {
						w.onWrite(event.Name)
						w.OnFileCreated(event.Name)
					}

				case info.Mode().IsDir():
					if w.OnDirCreated != nil {
						w.OnDirCreated(event.Name)
					}
				}

			case event.Op.Has(fsnotify.Chmod):
				if w.OnChmod != nil {
					w.OnChmod(event.Name)
				}
				w.onWrite(event.Name)

			case event.Op.Has(fsnotify.Remove):
				w.onRemoved(event.Name)
				if w.OnRemoved != nil {
					w.OnRemoved(event.Name)
				}
			}

		case e, ok := <-w.watcher.Errors:
			if !ok {
				return
			}

			if w.OnError != nil {
				w.OnError(e)
			}
		}
	}
}
