package debug

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/astralp2p/astrald/lib/paths"
)

// logDir is the crash-log directory. Empty writes no crash-log file.
var logDir string

var errNoLogDir = errors.New("no crash-log directory set")

// SetLogDir sets the crash-log directory. dir must be absolute. An empty dir writes no crash-log
// file.
func SetLogDir(dir string) error {
	if dir != "" {
		if err := paths.CheckAbs(dir); err != nil {
			return err
		}
	}

	logDir = dir
	return nil
}

// LogDir returns the crash-log directory, empty when none is set.
func LogDir() string {
	return logDir
}

// crashLogTimeLayout is the 24-hour timestamp of a crash log filename.
const crashLogTimeLayout = "20060102150405"

// SaveLog will save the crash log to a file, then invoke the provided function (if not nil) and panic again
// SaveLog writes the crash log to stderr when the file cannot be created.
func SaveLog(after func(p any)) {
	var p = recover()
	if p == nil {
		return
	}

	file, err := createCrashLog(logDir, time.Now())
	if err != nil {
		// why: a crash log that cannot be written must not swallow the panic
		fmt.Fprintf(os.Stderr, "failed to dump crash log: %v\n", err)
		fmt.Fprintf(os.Stderr, "panic: %v\n\n", p)
		os.Stderr.Write(debug.Stack())
	} else {
		defer file.Close()

		fmt.Fprintf(file, "panic: %v\n\n", p)
		file.Write(debug.Stack())

		fmt.Printf("crash log saved to %s\n", file.Name())
	}

	if after != nil {
		after(p)
	}

	panic(p)
}

// createCrashLog creates a new crash log file in dir named after t.
// note: the first crash log of a second is crash.<ts>.log, a later one is crash.<ts>.<n>.log
func createCrashLog(dir string, t time.Time) (*os.File, error) {
	// why: an empty dir would name a file in the working directory
	if dir == "" {
		return nil, errNoLogDir
	}

	var ts = t.Format(crashLogTimeLayout)
	for n := 0; ; n++ {
		var filename = "crash." + ts + ".log"
		if n > 0 {
			filename = fmt.Sprintf("crash.%s.%d.log", ts, n)
		}

		// why: O_EXCL keeps a same-second crash from truncating an earlier crash log
		file, err := os.OpenFile(filepath.Join(dir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if !errors.Is(err, fs.ErrExist) {
			return file, err
		}
	}
}

func SigInt(p any) {
	syscall.Kill(os.Getpid(), syscall.SIGINT)
	go func() {
		time.Sleep(3 * time.Second) // wait for clean exit
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}()
}
