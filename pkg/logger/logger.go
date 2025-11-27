package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.SugaredLogger

func Init(isDev bool) {
	var config zap.Config

	if isDev {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}

	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")

	logger, err := config.Build()
	if err != nil {
		panic(err)
	}

	Log = logger.Sugar()
}

func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}

// Convenience methods
func Info(args ...interface{})                    { Log.Info(args...) }
func Infof(template string, args ...interface{})  { Log.Infof(template, args...) }
func Error(args ...interface{})                   { Log.Error(args...) }
func Errorf(template string, args ...interface{}) { Log.Errorf(template, args...) }
func Debug(args ...interface{})                   { Log.Debug(args...) }
func Debugf(template string, args ...interface{}) { Log.Debugf(template, args...) }
func Warn(args ...interface{})                    { Log.Warn(args...) }
func Warnf(template string, args ...interface{})  { Log.Warnf(template, args...) }
func Fatal(args ...interface{})                   { Log.Fatal(args...); os.Exit(1) }
func Fatalf(template string, args ...interface{}) { Log.Fatalf(template, args...); os.Exit(1) }
