package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

type logger struct {
	debugLogger   *log.Logger
	infoLogger    *log.Logger
	warningLogger *log.Logger
	errorLogger   *log.Logger
	traceLogger   *log.Logger
}

func (log *logger) Trace(format string, v ...interface{}) {
	log.traceLogger.Output(2, fmt.Sprintf(format, v...))
}

func (log *logger) Debug(format string, v ...interface{}) {
	log.debugLogger.Output(2, fmt.Sprintf(format, v...))
}

func (log *logger) Info(format string, v ...interface{}) {
	log.infoLogger.Output(2, fmt.Sprintf(format, v...))
}

func (log *logger) Warning(format string, v ...interface{}) {
	log.warningLogger.Output(2, fmt.Sprintf(format, v...))
}

func (log *logger) Error(format string, v ...interface{}) {
	log.errorLogger.Output(2, fmt.Sprintf(format, v...))
}

// Log global log
var Log *logger

func Trace(format string, v ...interface{}) {
	Log.traceLogger.Output(2, fmt.Sprintf(format, v...))
}

func Debug(format string, v ...interface{}) {
	Log.debugLogger.Output(2, fmt.Sprintf(format, v...))
}

func Info(format string, v ...interface{}) {
	Log.infoLogger.Output(2, fmt.Sprintf(format, v...))
}

func Warning(format string, v ...interface{}) {
	Log.warningLogger.Output(2, fmt.Sprintf(format, v...))
}

func Error(format string, v ...interface{}) {
	Log.errorLogger.Output(2, fmt.Sprintf(format, v...))
}

func getWriters(level string) []io.Writer {
	level = strings.ToUpper(level)
	var levelList = []string{"ERROR", "WARNING", "INFO", "DEBUG", "TRACE"}
	var levelIoList = []io.Writer{os.Stderr, os.Stdout, os.Stdout, os.Stdout, os.Stdout}
	levelIndex := 10
	for index, l := range levelList {
		if level == l {
			levelIndex = index
		}
		if index > levelIndex {
			levelIoList[index] = io.Discard
		}
	}
	return levelIoList
}

// InitLog init log
func InitLog(level string) {
	writers := getWriters(level)
	Log = new(logger)
	const flag = log.Ldate | log.Ltime | log.Lmicroseconds | log.Lmsgprefix | log.Lshortfile
	Log.errorLogger = log.New(writers[0],
		"[ERROR] ",
		flag)
	Log.warningLogger = log.New(writers[1],
		"[WARNING] ",
		flag)
	Log.infoLogger = log.New(writers[2],
		"[INFO] ",
		flag)
	Log.debugLogger = log.New(writers[3],
		"[DEBUG] ",
		flag)
	Log.traceLogger = log.New(writers[4],
		"[TRACE] ",
		flag)
}
