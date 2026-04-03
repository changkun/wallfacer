package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_ReceivesCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	port := srv.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	// Send callback.
	url := fmt.Sprintf("http://127.0.0.1:%d/callback?code=abc123&state=xyz789", port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}

	result, err := srv.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Code != "abc123" {
		t.Errorf("Code = %q; want %q", result.Code, "abc123")
	}
	if result.State != "xyz789" {
		t.Errorf("State = %q; want %q", result.State, "xyz789")
	}
	if result.Error != "" {
		t.Errorf("Error = %q; want empty", result.Error)
	}
}

func TestCallbackServer_ReceivesError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	url := fmt.Sprintf("http://127.0.0.1:%d/?error=access_denied&error_description=User+denied", srv.Port())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d; want 400", resp.StatusCode)
	}

	result, err := srv.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Error != "access_denied" {
		t.Errorf("Error = %q; want %q", result.Error, "access_denied")
	}
	if result.ErrorDescription != "User denied" {
		t.Errorf("ErrorDescription = %q; want %q", result.ErrorDescription, "User denied")
	}
}

func TestCallbackServer_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	_, err = srv.Wait()
	if err == nil {
		t.Fatal("Wait should return error on timeout")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v; want context.DeadlineExceeded", err)
	}
}

func TestCallbackServer_Close(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}

	// Close immediately — Wait should return promptly.
	srv.Close()

	start := time.Now()
	_, err = srv.Wait()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Wait should return error after Close")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Wait took %v; should return promptly after Close", elapsed)
	}
}

func TestCallbackServer_CustomPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "/auth/callback")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	url := fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=custom-path&state=s1", srv.Port())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}

	result, err := srv.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Code != "custom-path" {
		t.Errorf("Code = %q; want %q", result.Code, "custom-path")
	}
}

func TestCallbackServer_FixedPort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find a free port to use as "fixed" port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixedPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv, err := NewCallbackServer(ctx, fixedPort, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	if srv.Port() != fixedPort {
		t.Errorf("port = %d; want %d", srv.Port(), fixedPort)
	}
}

func TestCallbackServer_BindFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Bind a port first, then try to bind same port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	_, err = NewCallbackServer(ctx, port, "")
	if err == nil {
		t.Fatal("expected error when port is already in use")
	}
}

func TestCallbackServer_BindsLocalhost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv, err := NewCallbackServer(ctx, 0, "")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	addr := srv.listener.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() {
		t.Errorf("listener IP = %v; want loopback (127.0.0.1)", addr.IP)
	}
}
