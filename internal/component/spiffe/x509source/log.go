package x509source

import (
	"fmt"
	"log/slog"
)

// logAdapter routes go-spiffe's printf-style logging to the component logger.
type logAdapter struct{ l *slog.Logger }

func (a logAdapter) Debugf(format string, args ...any) { a.l.Debug(fmt.Sprintf(format, args...)) }
func (a logAdapter) Infof(format string, args ...any)  { a.l.Info(fmt.Sprintf(format, args...)) }
func (a logAdapter) Warnf(format string, args ...any)  { a.l.Warn(fmt.Sprintf(format, args...)) }
func (a logAdapter) Errorf(format string, args ...any) { a.l.Error(fmt.Sprintf(format, args...)) }
