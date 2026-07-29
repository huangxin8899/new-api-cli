package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ANSI 转义序列。仅在输出到 TTY 且未禁用颜色时使用。
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// SupportsColor 判断某个流是否适合输出 ANSI 颜色。
// 遵循 NO_COLOR 约定（https://no-color.org）与 TERM=dumb。
func SupportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func wrap(code, s string) string { return code + s + ansiReset }

func bold(s string) string   { return wrap(ansiBold, s) }
func cyan(s string) string   { return wrap(ansiCyan, s) }
func dim(s string) string    { return wrap(ansiDim, s) }
func red(s string) string    { return wrap(ansiRed, s) }
func yellow(s string) string { return wrap(ansiYellow, s) }

// Emitter 的着色助手：Color 为 false 时原样返回，保证重定向后的输出干净。
func (e *Emitter) colorize(fn func(string) string, s string) string {
	if !e.Color {
		return s
	}
	return fn(s)
}

func (e *Emitter) dim(s string) string    { return e.colorize(dim, s) }
func (e *Emitter) red(s string) string    { return e.colorize(red, s) }
func (e *Emitter) yellow(s string) string { return e.colorize(yellow, s) }
func (e *Emitter) bold(s string) string   { return e.colorize(bold, s) }
