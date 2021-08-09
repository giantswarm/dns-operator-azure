package micrologr

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/go-logr/logr"

	"github.com/giantswarm/dns-operator-azure/pkg/errors"
)

type Config struct {
	Context     context.Context
	Micrologger micrologger.Logger

	Enabled bool
}

type Logger struct {
	context     context.Context
	micrologger micrologger.Logger

	enabled bool
}

func NewLogger(config Config) (logr.Logger, error) {
	if config.Context == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.Context must not be empty", config)
	}
	if config.Micrologger == nil {
		return nil, microerror.Maskf(errors.InvalidConfigError, "%T.Micrologger must not be empty", config)
	}

	l := &Logger{
		context:     config.Context,
		micrologger: config.Micrologger,
		enabled:     config.Enabled,
	}

	return l, nil
}

func (l *Logger) Enable() {
	l.enabled = true
}

func (l *Logger) Disable() {
	l.enabled = false
}

func (l *Logger) Enabled() bool {
	return l.enabled
}

func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	allKeysAndValues := []interface{}{"message", msg}
	allKeysAndValues = append(allKeysAndValues, keysAndValues...)
	l.micrologger.LogCtx(l.context, allKeysAndValues...)
}

func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.Info(msg, keysAndValues...)
	l.micrologger.Errorf(l.context, err, msg)
}

func (l *Logger) V(_ int) logr.InfoLogger {
	return l
}

func (l *Logger) WithValues(keysAndValues ...interface{}) logr.Logger {
	newLogger := &Logger{
		context:     l.context,
		micrologger: l.micrologger.With(keysAndValues...),
		enabled:     l.enabled,
	}

	return newLogger
}

func (l *Logger) WithName(name string) logr.Logger {
	return l.WithValues("name", name)
}
