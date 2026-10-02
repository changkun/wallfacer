package webserver

import (
	"net/http"
	"slices"
	"strings"

	"latere.ai/x/pkg/httpjson"
)

// The catch-all patterns MountCatchAll registers. spaCatchAll takes every path
// no other route matches; apiCatchAll and apiRoot take the unmatched paths
// under /api, which an API client must see answered as an API error rather
// than with the SPA shell. All three are method-agnostic: ServeMux treats
// "GET /" and "/api/" as conflicting (each matches requests the other does
// not, and neither is more specific), so the SPA catch-all cannot carry a
// method once the API catch-all exists. apiRoot is registered as an exact
// pattern so a request for "/api" is answered, not redirected to "/api/".
const (
	spaCatchAll = "/"
	apiCatchAll = "/api/"
	apiRoot     = "/api"
)

// The errors an unmatched /api path answers with: one code and the one user
// sentence for each. The method, path and allowed methods travel in the
// envelope's details.
const (
	codeAPINotFound            = "not_found"
	messageAPINotFound         = "No API endpoint exists at this path."
	codeAPIMethodNotAllowed    = "method_not_allowed"
	messageAPIMethodNotAllowed = "This API endpoint does not accept the request method."
)

// probeMethods are the methods allowedMethods asks the mux about, in the order
// an Allow header lists them.
var probeMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// MountCatchAll registers the catch-all routes of a server that serves the SPA
// beside an API: serveIndex answers GET and HEAD on every path no other route
// matches, so a hard load of a client route renders the app, and an unmatched
// path under /api answers 404 (or 405 when a route serves the path under other
// methods) in the error envelope, never the shell. Every route registered with
// a path more specific than "/" or "/api/" keeps its requests, whenever it is
// registered.
func MountCatchAll(mux *http.ServeMux, serveIndex http.HandlerFunc) {
	mux.HandleFunc(spaCatchAll, spaFallback(mux, serveIndex))
	mux.HandleFunc(apiCatchAll, apiFallback(mux))
	mux.HandleFunc(apiRoot, apiFallback(mux))
}

// allowedMethods returns the methods for which a route other than the
// catch-all patterns serves r's path. A method-agnostic catch-all matches
// every method, so ServeMux no longer answers a known path's wrong method with
// its own 405; the catch-all handlers call this to answer it themselves.
func allowedMethods(mux *http.ServeMux, r *http.Request) []string {
	var allow []string
	for _, m := range probeMethods {
		probe := r.Clone(r.Context())
		probe.Method = m
		_, pattern := mux.Handler(probe)
		switch pattern {
		case "", spaCatchAll, apiCatchAll, apiRoot:
			continue
		}
		allow = append(allow, m)
	}
	return allow
}

// apiFallback answers a request under /api that no route serves: 405 with an
// Allow header when a route serves the path under other methods, else 404,
// both in the error envelope.
func apiFallback(mux *http.ServeMux) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if allow := allowedMethods(mux, r); len(allow) > 0 {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			httpjson.WriteError(w, http.StatusMethodNotAllowed, httpjson.Error{
				Code:    codeAPIMethodNotAllowed,
				Message: messageAPIMethodNotAllowed,
				Details: map[string]any{"method": r.Method, "path": r.URL.Path, "allow": allow},
			})
			return
		}
		httpjson.WriteError(w, http.StatusNotFound, httpjson.Error{
			Code:    codeAPINotFound,
			Message: messageAPINotFound,
			Details: map[string]any{"method": r.Method, "path": r.URL.Path},
		})
	}
}

// spaFallback serves the SPA shell for GET and HEAD on every path no other
// route matches. Other methods answer 405 with an Allow header naming GET,
// HEAD and any method a route serves on the path, as ServeMux answered when
// the catch-all was "GET /".
func spaFallback(mux *http.ServeMux, serveIndex http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			serveIndex(w, r)
			return
		}
		allow := []string{http.MethodGet, http.MethodHead}
		for _, m := range allowedMethods(mux, r) {
			if !slices.Contains(allow, m) {
				allow = append(allow, m)
			}
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
