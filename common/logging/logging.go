package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

const (
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
)

type coloredTimeWriter struct {
	out io.Writer
}

func (w coloredTimeWriter) Write(p []byte) (int, error) {
	now := time.Now()
	prefix := fmt.Sprintf("%s%s%s %s%s%s ",
		colorCyan,
		now.Format("2006/01/02"),
		colorReset,
		colorYellow,
		now.Format("15:04:05"),
		colorReset,
	)
	_, err := fmt.Fprintf(w.out, "%s%s", prefix, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func InitColoredStdLogger() {
	log.SetFlags(0)
	log.SetOutput(coloredTimeWriter{out: os.Stderr})
}
