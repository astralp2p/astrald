package shell

import (
	"bufio"
	"errors"
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
)

type Terminal struct {
	scanner *bufio.Scanner
	rw      io.ReadWriter
}

func NewTerminal(rw io.ReadWriter) *Terminal {
	return &Terminal{
		rw:      rw,
		scanner: bufio.NewScanner(rw),
	}
}

func (e *Terminal) ReadLine() (line string, err error) {
	if e.scanner.Scan() {
		line = e.scanner.Text()
		return
	}

	if e.scanner.Err() == nil {
		return "", io.EOF
	}

	return "", e.scanner.Err()
}

func (e *Terminal) Read(p []byte) (n int, err error) {
	return e.rw.Read(p)
}

func (e *Terminal) Write(p []byte) (n int, err error) {
	return e.rw.Write(p)
}

func (e *Terminal) Close() error {
	if c, ok := e.rw.(io.Closer); ok {
		return c.Close()
	}
	return errors.New("rw does not implement io.Closer")
}

func (e *Terminal) Printf(f string, v ...interface{}) error {

	_, err := e.rw.Write([]byte(fmt.Sprintf(f, v...)))
	return err
}

func (e *Terminal) Print(objects ...astral.Object) (err error) {
	for _, object := range objects {
		_, err = fmt.Fprint(e.rw, object)
		if err != nil {
			return
		}
	}
	return
}
