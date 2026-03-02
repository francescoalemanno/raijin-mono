package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// callbackResult is the code+state received by the local OAuth callback server.
type callbackResult struct {
	Code  string
	State string
}

// callbackServer is a minimal local HTTP server that listens for a single
// OAuth redirect and returns the code+state to the caller.
type callbackServer struct {
	server   *http.Server
	resultCh chan callbackResult
}

// startCallbackServer starts a local HTTP server on addr (e.g. "127.0.0.1:8085")
// listening at path (e.g. "/oauth2callback").
// Call waitForCode to block until the browser redirects back, then close.
func startCallbackServer(addr, path string) (*callbackServer, error) {
	cs := &callbackServer{
		resultCh: make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		errParam := q.Get("error")
		if errParam != "" {
			http.Error(w, "Authentication failed: "+errParam, http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if code == "" {
			http.Error(w, "Missing code parameter.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body><h1>Authentication Successful</h1><p>You can close this window and return to the terminal.</p></body></html>")
		select {
		case cs.resultCh <- callbackResult{Code: code, State: state}:
		default:
		}
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("oauth callback: listen %s: %w", addr, err)
	}

	cs.server = &http.Server{Handler: mux}
	go cs.server.Serve(ln) //nolint:errcheck

	return cs, nil
}

// waitForCode blocks until either the browser delivers the OAuth callback
// or ctx is done. Returns (result, true) on success, (zero, false) on
// cancellation/timeout.
func (cs *callbackServer) waitForCode(ctx context.Context) (callbackResult, bool) {
	select {
	case r := <-cs.resultCh:
		return r, true
	case <-ctx.Done():
		return callbackResult{}, false
	}
}

// close shuts down the HTTP server gracefully.
func (cs *callbackServer) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cs.server.Shutdown(ctx) //nolint:errcheck
}
