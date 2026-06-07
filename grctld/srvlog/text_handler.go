package srvlog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

var sourceRoot = detectSourceRoot()

func detectSourceRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd + "/"
}

type textHandler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Leveler

	addSource bool
	attrs     []slog.Attr
}

func newTextHandler(w io.Writer, opts *slog.HandlerOptions) *textHandler {
	h := &textHandler{
		w:         w,
		level:     slog.LevelInfo,
		addSource: false,
	}
	if opts != nil {
		if opts.Level != nil {
			h.level = opts.Level
		}
		h.addSource = opts.AddSource
	}
	return h
}

func (h *textHandler) clone() *textHandler {
	return &textHandler{
		w:         h.w,
		level:     h.level,
		addSource: h.addSource,
		attrs:     h.attrs,
	}
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	buf = r.Time.AppendFormat(buf, timeFormat)
	buf = append(buf, ' ')
	buf = appendLevel(buf, r.Level)
	buf = append(buf, ' ')

	if h.addSource && r.PC != 0 {
		if src := r.Source(); src != nil {
			file := src.File
			if trimmed := strings.TrimPrefix(file, sourceRoot); trimmed != file {
				file = trimmed
			}
			buf = append(buf, '[')
			buf = append(buf, file...)
			buf = append(buf, ':')
			buf = fmt.Appendf(buf, "%d", src.Line)
			buf = append(buf, ']')
			buf = append(buf, ' ')
		}
	}

	buf = append(buf, r.Message...)

	for _, a := range h.attrs {
		buf = appendAttr(buf, a)
	}

	r.Attrs(func(a slog.Attr) bool {
		buf = appendAttr(buf, a)
		return true
	})

	buf = append(buf, '\n')

	h.mu.Lock()
	_, err := h.w.Write(buf)
	h.mu.Unlock()
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

func (h *textHandler) WithGroup(_ string) slog.Handler {
	return h.clone()
}

func appendLevel(buf []byte, level slog.Level) []byte {
	switch {
	case level < slog.LevelInfo:
		buf = append(buf, "DEBUG"...)
	case level < slog.LevelWarn:
		buf = append(buf, "INFO"...)
	case level < slog.LevelError:
		buf = append(buf, "WARN"...)
	default:
		buf = append(buf, "ERROR"...)
	}
	return buf
}

func appendAttr(buf []byte, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		return buf
	}
	buf = append(buf, ' ')
	buf = append(buf, a.Key...)
	buf = append(buf, '=')
	buf = append(buf, a.Value.String()...)
	return buf
}
