// Package logpipe manages subprocess stdout as a line-by-line channel stream.
//
// When streaming container logs to the browser via SSE, the server needs to read
// subprocess output line by line without blocking the main goroutine. [Pipe]
// starts a subprocess, captures its stdout (optionally merging stderr), and
// delivers lines through a channel. This consolidates the repeated pattern of
// pipe creation, buffered scanning, and process wait into a single abstraction.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed by [handler] for streaming
// live container logs to the browser via Server-Sent Events. Changes to buffer
// sizing or line handling affect log streaming latency and memory usage.
//
// # Usage
//
//	p, err := logpipe.Start(exec.Command("podman", "logs", "-f", name))
//	for line := range p.Lines() {
//	    fmt.Fprintf(w, "data: %s\n\n", line)
//	}
//	<-p.Done()
package logpipe
