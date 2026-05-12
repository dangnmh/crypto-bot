package logger

import "log/slog"

type MultiHandlerForTest = multiHandler

func NewMultiHandlerForTest(handlers ...slog.Handler) *MultiHandlerForTest {
	return &multiHandler{handlers: handlers}
}
