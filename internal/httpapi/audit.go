package httpapi

import (
	"log/slog"
	"net/http"
)

type ownerAuditWriter struct {
	http.ResponseWriter
	status int
}

func (w *ownerAuditWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *ownerAuditWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func ownerAudit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := &ownerAuditWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(record, r)
		slog.Info("owner action", "route", r.Pattern, "method", r.Method, "http_status", record.status, "model_id", r.PathValue("model"), "provider_id", r.PathValue("provider"))
	})
}
