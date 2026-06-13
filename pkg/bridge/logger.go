package bridge

// Logger is a minimal logging interface used by the library.
//
// It is intentionally small so that consumers are not forced to depend on a
// specific logging implementation. Popular loggers such as logrus satisfy this
// interface out of the box (e.g. *logrus.Logger and logrus.FieldLogger).
//
// When no logger is provided the library stays silent by using a no-op logger.
type Logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// nopLogger is a Logger that discards all messages. It is used as the default
// when no logger is supplied, so the library never writes to a global logger.
type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Warnf(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}

// NopLogger returns a Logger that discards all messages. It is useful for
// SessionProvider implementations that want a non-nil logger default.
func NopLogger() Logger {
	return nopLogger{}
}

// orNop returns the provided logger, or a no-op logger when it is nil.
func orNop(l Logger) Logger {
	if l == nil {
		return nopLogger{}
	}
	return l
}
