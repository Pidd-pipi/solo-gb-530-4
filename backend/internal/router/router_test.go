package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"radiation-dose-budget-control/backend/internal/config"
)

func TestNewRegistersBudgetOccupationRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:router-occupation-530?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	cfg := config.Config{
		JWTSecret:          "router-occupation-test-secret-32-bytes",
		JWTTTL:             time.Hour,
		CORSOrigin:         "http://localhost:18530",
		RateLimitPerMinute: 240,
		Thresholds: config.ThresholdConfig{
			DefaultAnnualLimitMSV: 20, DefaultAdminLimitMSV: 12,
			NearLegalRatio: 0.9, Version: "ALARA-2026.1",
		},
	}
	engine := New(db, cfg)
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/budget-occupations"},
		{http.MethodGet, "/api/v1/budget-occupations/worker-balances"},
		{http.MethodGet, "/api/v1/budget-occupations/1"},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code == http.StatusNotFound {
			t.Fatalf("%s %s resolved to 404; route registration conflict", tc.method, tc.path)
		}
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token = %d, want 401", tc.method, tc.path, recorder.Code)
		}
	}
}
