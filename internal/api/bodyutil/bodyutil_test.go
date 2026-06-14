package bodyutil

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLimitBodyMiddlewareRejectsOversizedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(8))
	r.POST("/management", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/management", bytes.NewReader([]byte(`{"value":"too-large"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestLimitBodyMiddlewareRestoresBodyForHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(64))
	r.POST("/management", func(c *gin.Context) {
		body, err := ReadRequestBody(c, 64)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		c.String(http.StatusOK, string(body))
	})

	req := httptest.NewRequest(http.MethodPost, "/management", bytes.NewReader([]byte(`{"value":"ok"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if got := w.Body.String(); got != `{"value":"ok"}` {
		t.Fatalf("unexpected response body: %s", got)
	}
}

func TestLimitBodyMiddlewareRejectsOversizedDeleteRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(LimitBodyMiddleware(8))
	r.DELETE("/management", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodDelete, "/management", bytes.NewReader([]byte(`{"value":"too-large"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}
}

func TestReadRequestBodyUsesCacheAndSetRequestBodyRefreshesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"value":"original"}`))

	body, err := ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(body) != `{"value":"original"}` {
		t.Fatalf("body = %s", body)
	}

	c.Request.Body = io.NopCloser(strings.NewReader(`{"value":"mutated-without-cache-refresh"}`))
	body, err = ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected cached read error: %v", err)
	}
	if string(body) != `{"value":"original"}` {
		t.Fatalf("cached body = %s", body)
	}

	SetRequestBody(c, []byte(`{"value":"updated"}`))
	body, err = ReadRequestBody(c, 64)
	if err != nil {
		t.Fatalf("unexpected refreshed read error: %v", err)
	}
	if string(body) != `{"value":"updated"}` {
		t.Fatalf("refreshed body = %s", body)
	}
}

func TestModelRequestBodyLimitCanBeConfigured(t *testing.T) {
	previous := ModelRequestBodyLimit()
	t.Cleanup(func() { SetModelRequestBodyLimit(previous) })

	SetModelRequestBodyLimit(32)
	if got := ModelRequestBodyLimit(); got != 32 {
		t.Fatalf("ModelRequestBodyLimit = %d, want 32", got)
	}

	SetModelRequestBodyLimit(0)
	if got := ModelRequestBodyLimit(); got != DefaultModelBodyLimit {
		t.Fatalf("ModelRequestBodyLimit default = %d, want %d", got, DefaultModelBodyLimit)
	}
}
