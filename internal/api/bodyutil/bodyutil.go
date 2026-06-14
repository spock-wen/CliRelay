package bodyutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const (
	DefaultRequestBodyLimit   int64 = 16 << 20
	DefaultModelBodyLimit     int64 = 128 << 20
	ManagementBodyLimit       int64 = 2 << 20
	ConfigYAMLBodyLimit       int64 = 2 << 20
	AuthFileBodyLimit         int64 = 2 << 20
	VertexCredentialBodyLimit int64 = 2 << 20
)

var ErrBodyTooLarge = errors.New("request body too large")

const requestBodyCacheKey = "cliproxy.request_body_cache"

var modelRequestBodyLimit atomic.Int64

func init() {
	modelRequestBodyLimit.Store(DefaultModelBodyLimit)
}

// SetModelRequestBodyLimit configures the request-body limit used by model API endpoints.
func SetModelRequestBodyLimit(limit int64) {
	if limit <= 0 {
		limit = DefaultModelBodyLimit
	}
	modelRequestBodyLimit.Store(limit)
}

// ModelRequestBodyLimit returns the active request-body limit for model API endpoints.
func ModelRequestBodyLimit() int64 {
	limit := modelRequestBodyLimit.Load()
	if limit <= 0 {
		return DefaultModelBodyLimit
	}
	return limit
}

func normalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultRequestBodyLimit
	}
	return limit
}

func cachedRequestBody(c *gin.Context) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	bodyVal, ok := c.Get(requestBodyCacheKey)
	if !ok || bodyVal == nil {
		return nil, false
	}
	body, ok := bodyVal.([]byte)
	return body, ok
}

// SetRequestBody caches and restores a request body for downstream consumers.
func SetRequestBody(c *gin.Context, body []byte) {
	if c == nil || c.Request == nil {
		return
	}
	c.Set(requestBodyCacheKey, body)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
}

// ReadRequestBody reads and restores an incoming HTTP request body with a strict size limit.
func ReadRequestBody(c *gin.Context, limit int64) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}

	limit = normalizeLimit(limit)
	if cached, ok := cachedRequestBody(c); ok {
		if int64(len(cached)) > limit {
			return nil, ErrBodyTooLarge
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(cached))
		c.Request.ContentLength = int64(len(cached))
		return cached, nil
	}

	if c.Writer == nil {
		body, err := ReadAll(c.Request.Body, limit)
		if err != nil {
			return nil, err
		}
		SetRequestBody(c, body)
		return body, nil
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	SetRequestBody(c, body)
	return body, nil
}

// ReadAll reads from any reader with a strict size limit.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	limit = normalizeLimit(limit)
	limited := &io.LimitedReader{R: r, N: limit + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func IsTooLarge(err error) bool {
	if errors.Is(err, ErrBodyTooLarge) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var timeoutErr interface{ Timeout() bool }
	return errors.As(err, &timeoutErr) && timeoutErr.Timeout()
}

// LimitBodyMiddleware eagerly reads and restores request bodies with a hard limit.
// It is intended for small management JSON payloads so downstream binders can reuse the body safely.
func LimitBodyMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		if !shouldLimitRequestBody(c.Request) {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit && limit > 0 {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if _, err := ReadRequestBody(c, limit); err != nil {
			if IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		c.Next()
	}
}

func shouldLimitRequestBody(req *http.Request) bool {
	if req == nil || req.Body == nil {
		return false
	}
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Type")))
	return !strings.HasPrefix(contentType, "multipart/form-data")
}
