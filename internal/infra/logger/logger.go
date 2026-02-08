package logger

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes for terminal output.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
)

// defaultWidth is the fixed padding width for module names (Docker Compose style).
const defaultWidth = 10

// Module is a lightweight logger that prints messages with a colored module prefix.
// Styled after Docker Compose's per-container output format.
//
//	discord  | connected as Bot#1234
//	http     | listening on :8080
type Module struct {
	name  string
	color string
}

// New creates a module logger with the given name and ANSI color.
func New(name, color string) *Module {
	return &Module{name: name, color: color}
}

// Logf prints a formatted message with the colored module prefix.
func (m *Module) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	padded := m.name + strings.Repeat(" ", max(1, defaultWidth-len(m.name)))
	fmt.Fprintf(os.Stderr, "%s%s%s%s| %s%s\n", m.color, Bold, padded, Reset+Dim, Reset, msg)
}

// Log prints a plain message with the colored module prefix.
func (m *Module) Log(msg string) {
	m.Logf("%s", msg)
}

// Warnf prints a warning (yellow) message.
func (m *Module) Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	padded := m.name + strings.Repeat(" ", max(1, defaultWidth-len(m.name)))
	fmt.Fprintf(os.Stderr, "%s%s%s%s| %s%s%s\n", m.color, Bold, padded, Reset+Dim, Reset+Yellow, msg, Reset)
}

// Errorf prints an error (red) message.
func (m *Module) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	padded := m.name + strings.Repeat(" ", max(1, defaultWidth-len(m.name)))
	fmt.Fprintf(os.Stderr, "%s%s%s%s| %s%s%s\n", m.color, Bold, padded, Reset+Dim, Reset+Red, msg, Reset)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
