package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// fileOutput is shared by standard, GORM and Gin log writers.
var fileOutput struct {
	sync.Mutex
	daily *dailyWriter
}

type dailyWriter struct {
	dir  string
	date string
	file *os.File
	now  func() time.Time
}

func (w *dailyWriter) rotate() error {
	date := w.now().Format("2006-01-02")
	if w.file != nil && date == w.date {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(w.dir, date+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	old := w.file
	w.file, w.date = f, date
	if old != nil {
		return old.Close()
	}
	return nil
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	if err := w.rotate(); err != nil {
		return 0, err
	}
	n, err := w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = w.file.Close()
		w.file = nil // Retry opening on the next record; never replay a partial record.
	}
	return n, err
}

// InitDailyFile enables daily files for all logging package writers.
// Call before initializing application dependencies. The returned function closes
// the file and restores console-only output.
func InitDailyFile(dir string) (func() error, error) {
	fileOutput.Lock()
	defer fileOutput.Unlock()
	if fileOutput.daily != nil {
		return nil, errors.New("daily logging already initialized")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	w := &dailyWriter{dir: dir, now: time.Now}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	fileOutput.daily = w
	return func() error {
		fileOutput.Lock()
		defer fileOutput.Unlock()
		if fileOutput.daily != w {
			return nil
		}
		fileOutput.daily = nil
		if w.file != nil {
			return w.file.Close()
		}
		return nil
	}, nil
}

type outputWriter struct{ console io.Writer }

// NewOutputWriter duplicates already formatted records without adding a timestamp.
func NewOutputWriter(console io.Writer) io.Writer { return outputWriter{console: console} }

func (w outputWriter) Write(p []byte) (int, error) {
	fileOutput.Lock()
	defer fileOutput.Unlock()
	n, consoleErr := w.console.Write(p)
	if consoleErr == nil && n != len(p) {
		consoleErr = io.ErrShortWrite
	}
	var fileErr error
	if fileOutput.daily != nil {
		_, fileErr = fileOutput.daily.Write(ansiColor.ReplaceAll(p, nil))
		if fileErr != nil {
			// Write directly to stderr to avoid recursively entering the logging pipeline.
			fmt.Fprintf(os.Stderr, "%s [logging] write daily log failed: %v\n", time.Now().Format("2006/01/02 15:04:05"), fileErr)
		}
	}
	return n, errors.Join(consoleErr, fileErr)
}
