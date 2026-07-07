package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/streams"
)

type LogFile struct {
	mu   sync.Mutex
	ch   *channel.Channel
	path string
}

var _ log.EntryLogger = &LogFile{}

func CreateLogFile() (*LogFile, error) {
	f := &LogFile{}

	f.path = "astrald.log." + time.Now().Format("2006-01-02_15-04-05")
	if home, err := os.UserHomeDir(); err == nil {
		dirPath := filepath.Join(home, ".config", "astrald", "logs")
		os.MkdirAll(dirPath, 0750)
		f.path = filepath.Join(dirPath, f.path)
	}

	logFile, err := os.Create(f.path)
	if err != nil {
		return nil, err
	}

	f.ch = channel.New(streams.ReadWriteCloseSplit{
		Reader: nil,
		Writer: logFile,
		Closer: nil,
	})

	return f, nil
}

func (l LogFile) LogEntry(entry *log.Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	err := l.ch.Send(entry)
	if err != nil {
		fmt.Println("log write error:", err)
	}
}
