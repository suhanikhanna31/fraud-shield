package api

import (
	"context"
	"net/http"
	"time"
)

// contextWithTimeout derives a bounded context from the incoming request so
// slow downstream calls (DB queries, etc.) can't hang a handler forever.
func contextWithTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 3*time.Second)
}
