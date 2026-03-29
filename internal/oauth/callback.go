package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// CallbackResult holds the values extracted from an OAuth callback redirect.
type CallbackResult struct {
	Code             string
	State            string
	Error            string
	ErrorDescription string
}

// CallbackServer is an ephemeral localhost HTTP listener that receives
// exactly one OAuth callback redirect, then shuts down.
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	resultCh chan CallbackResult
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	ctx      context.Context
}

// NewCallbackServer binds a listener on 127.0.0.1:0 (random port) and
// starts an HTTP server that accepts exactly one callback request.
// The caller should wrap ctx with context.WithTimeout to enforce a deadline.
func NewCallbackServer(ctx context.Context) (*CallbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("oauth callback: bind failed: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	resultCh := make(chan CallbackResult, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	cs := &CallbackServer{
		listener: ln,
		server:   srv,
		resultCh: resultCh,
		cancel:   cancel,
		ctx:      ctx,
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result := CallbackResult{
			Code:             q.Get("code"),
			State:            q.Get("state"),
			Error:            q.Get("error"),
			ErrorDescription: q.Get("error_description"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if result.Error != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, callbackPageHTML, "Authorization failed: "+result.Error+". "+result.ErrorDescription)
		} else {
			_, _ = fmt.Fprintf(w, callbackPageHTML, "Authorization successful. You can close this tab.")
		}

		select {
		case resultCh <- result:
		default:
		}

		// Shut down after responding.
		go func() { _ = srv.Shutdown(context.Background()) }()
	})

	cs.wg.Add(1)
	go func() {
		defer cs.wg.Done()
		_ = srv.Serve(ln)
	}()

	return cs, nil
}

// Port returns the port the callback server is listening on.
func (s *CallbackServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Wait blocks until a callback result is received or the context expires.
func (s *CallbackServer) Wait() (CallbackResult, error) {
	select {
	case result := <-s.resultCh:
		s.wg.Wait()
		return result, nil
	case <-s.ctx.Done():
		_ = s.server.Close()
		s.wg.Wait()
		return CallbackResult{}, s.ctx.Err()
	}
}

// Close force-closes the listener and cancels the context.
func (s *CallbackServer) Close() {
	s.cancel()
	_ = s.server.Close()
}

const callbackPageHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Wallfacer</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#1a1917;color:#f0ede6}p{font-size:1.1em;text-align:center}</style>
</head><body><p>%s</p></body></html>`
