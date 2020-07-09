package parallelism

import (
	"runtime"
)

type ErroneousSystemLogInfo struct {
	Message    []byte
	Stacktrace []byte
}

/*
Because the tests will run in parallel and need to have their logs captured independently so they don't get all jumbled,
we expect the developer to write to test-specific loggers rather than the system logger. Developers might still forget,
however, so we need a way to:

1) loudly remind them in case they slip up and use the system-level logger but
2) not crash the program, because code we don't control (like the Docker client) use logrus too (we tried panicking on
  system-level log write, but that didn't work because the Docker client uses logrus too)

So, we have this special writer which doesn't actually write to STDOUT but captures the input for later retrieval.
 */
type ErroneousSystemLogCaptureWriter struct {
	logMessagesPtr *[]ErroneousSystemLogInfo
}

/*
Constructs a new capturer for erroneous system log messages. The Write interface (which we're trying to implement) doesn't
allow a pointer receiver, so we have to go through these gymnastics of a slice pointer instead.
 */
func NewErroneousSystemLogCaptureWriter(logMessagesPtr *[]ErroneousSystemLogInfo) *ErroneousSystemLogCaptureWriter {
	return &ErroneousSystemLogCaptureWriter{
		logMessagesPtr: logMessagesPtr,
	}
}

func (writer ErroneousSystemLogCaptureWriter) Write(data []byte) (n int, err error) {
	stacktraceBytes := getStacktraceBytes()
	logInfo := ErroneousSystemLogInfo{
		Message:       data,
		Stacktrace: stacktraceBytes,
	}

	// In order to match the Write interface we can't have the receiver be a pointer, so we have to go through these gymnastics
	*(writer.logMessagesPtr) = append(*(writer.logMessagesPtr), logInfo)
	return len(data), nil
}

func getStacktraceBytes() []byte {
	// This code is almost exactly from debug.PrintStack
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return buf[0:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}
