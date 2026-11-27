package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

type logEntry struct {
	method     string
	path       string
	remoteAddr string
	// principal is the caller that the request was authenticated as, nil when it was
	// not authenticated. It is set by logPrincipal, after the entry was created.
	principal *tenancy.Principal
}

func (l *logEntry) Write(status, bytes int, header http.Header, elapsed time.Duration, extra interface{}) {
	attrs := []any{slog.String("remote_addr", l.remoteAddr), slog.Int("status", status), slog.Int("bytes", bytes), slog.Duration("duration", elapsed)}
	if p := l.principal; p != nil {
		attrs = append(attrs, slog.String("sub", p.Subject), slog.String("act", p.Actor), slog.String("org", p.OrgId), slog.String("jti", p.Jti))
	}
	slog.Info(fmt.Sprintf("%s %s", l.method, l.path), attrs...)
}

func (l *logEntry) Panic(v interface{}, stack []byte) {
	slog.Info(fmt.Sprintf("%s %s", l.method, l.path), slog.String("remote_addr", l.remoteAddr), slog.Any("panic", v), slog.String("stack", string(stack)))
}

type logFormatter struct{}

func (l logFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return &logEntry{
		method:     r.Method,
		path:       r.URL.Path,
		remoteAddr: r.RemoteAddr,
	}
}

// logPrincipal adds the caller of an authenticated request to the log entry of the
// request. The request logger runs before the authentication, on a request that does
// not have the caller in its context yet, so it cannot read the caller itself.
func logPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if entry, ok := middleware.GetLogEntry(r).(*logEntry); ok {
			if p, ok := tenancy.FromContext(r.Context()); ok {
				entry.principal = p
			}
		}
		next.ServeHTTP(w, r)
	})
}
