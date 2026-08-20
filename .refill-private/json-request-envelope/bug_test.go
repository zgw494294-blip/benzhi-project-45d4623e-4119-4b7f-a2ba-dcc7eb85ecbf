package jsonrequestenvelope

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	web "community-inspection/internal/http"
	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

func TestJSONRequestEnvelopeRejectsUnknownTrailingAndEmptyBodies(t *testing.T) {
	newHandler := func(t *testing.T) http.Handler {
		t.Helper()
		repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
		if err := repository.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		sequence := 0
		svc, err := service.New(repository, service.Config{
			Now:   func() time.Time { return time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC) },
			NewID: func(prefix string) string { sequence++; return prefix + "-" + string(rune('a'+sequence)) },
		})
		if err != nil {
			t.Fatal(err)
		}
		return web.NewHandler(svc)
	}

	t.Run("unknown-field", func(t *testing.T) {
		body := []byte(`{"name":"严格请求","area":"东区","scheduledDate":"2026-08-20","sites":[{"name":"路灯","category":"照明","location":"东门"}],"unexpected":"value"}`)
		request := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
		response := httptest.NewRecorder()
		newHandler(t).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("未知字段返回状态 %d", response.Code)
		}
	})

	t.Run("trailing-object", func(t *testing.T) {
		body := []byte(`{"name":"严格请求","area":"东区","scheduledDate":"2026-08-20","sites":[{"name":"路灯","category":"照明","location":"东门"}]} {"again":true}`)
		request := httptest.NewRequest(http.MethodPost, "/api/plans", bytes.NewReader(body))
		response := httptest.NewRecorder()
		newHandler(t).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("拼接对象返回状态 %d", response.Code)
		}
	})

	t.Run("empty-body", func(t *testing.T) {
		request := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/api/plans"}}
		response := httptest.NewRecorder()
		newHandler(t).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("空请求体返回状态 %d", response.Code)
		}
	})
}
