package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type contextKey int

const (
	userKey contextKey = iota
	requestKey
)

func userID(r *http.Request) int64 {
	id, _ := r.Context().Value(userKey).(int64)
	return id
}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(r.Header.Values("Authorization")) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			h.fail(w, r, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}

		id, err := h.auth.Verify(parts[1])
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			h.fail(w, r, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}

		if !h.apiLimiter.allow(strconv.FormatInt(id, 10), time.Now()) {
			h.tooMany(w, r, h.apiLimiter.limit)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, id)))
	})
}

func (h *handler) limitAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		if !h.authLimiter.allow(ip, time.Now()) {
			h.tooMany(w, r, h.authLimiter.limit)
			return
		}

		next(w, r)
	}
}

func (h *handler) tooMany(w http.ResponseWriter, r *http.Request, limit int) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, (60+limit-1)/limit)))
	h.fail(w, r, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded")
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (h *handler) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var id [16]byte
		_, _ = rand.Read(id[:])
		requestID := hex.EncodeToString(id[:])
		r = r.WithContext(context.WithValue(r.Context(), requestKey, requestID))

		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")

		recorder := &responseWriter{ResponseWriter: w}
		defer h.finishRequest(recorder, r, start)

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := validateQuery(r.URL.RawQuery); err != nil {
			h.fail(recorder, r, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}

		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

func (h *handler) finishRequest(w *responseWriter, r *http.Request, start time.Time) {
	requestID := r.Context().Value(requestKey)
	if recovered := recover(); recovered != nil {
		h.logger.Error("request panic", "request_id", requestID, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
		if w.status == 0 {
			h.fail(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
	}

	status := w.status
	if status == 0 {
		status = http.StatusOK
	}

	h.logger.Info("http request",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", float64(time.Since(start).Microseconds())/1000,
		"bytes", w.bytes,
	)
}
