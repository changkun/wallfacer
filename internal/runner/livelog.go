package runner

import "changkun.de/x/wallfacer/internal/pkg/livelog"

// liveLog wraps livelog.Log for backward compatibility within the runner package.
type liveLog = livelog.Log

// LiveLogReader wraps livelog.Reader for backward compatibility.
// The handler package references this type via the runner.Interface.
type LiveLogReader = livelog.Reader

func newLiveLog() *liveLog {
	return livelog.New()
}
