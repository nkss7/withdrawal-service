//go:build integration

package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nikitafox/withdrawal-service/internal/controller"
	"github.com/nikitafox/withdrawal-service/internal/domain"
)

func baseURL() string {
	if url := os.Getenv("SERVICE_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

func authToken() string {
	if token := os.Getenv("AUTH_TOKEN"); token != "" {
		return token
	}
	return "test-secret-token"
}

func setupDB(t *testing.T) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	_, err = db.Exec(`DELETE FROM ledger_entries`)
	if err != nil {
		t.Fatalf("failed to cleanup ledger_entries: %v", err)
	}
	_, err = db.Exec(`DELETE FROM withdrawals`)
	if err != nil {
		t.Fatalf("failed to cleanup withdrawals: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO balances (user_id, amount, currency) VALUES ($1, 1000.00000000, 'USDT')
		 ON CONFLICT (user_id) DO UPDATE SET amount = 1000.00000000`,
		"00000000-0000-0000-0000-000000000001",
	)
	if err != nil {
		t.Fatalf("failed to seed test data: %v", err)
	}
}

func makeRequest(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	url := baseURL() + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send request to %s: %v", url, err)
	}
	return resp
}

func parseResponse[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nbody: %s", err, string(body))
	}
	return result
}

func TestCreateWithdrawalSuccess(t *testing.T) {
	setupDB(t)

	payload := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "50",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-success-1",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 201, got %d, body: %s", resp.StatusCode, string(body))
	}

	w := parseResponse[controller.WithdrawalResponse](t, resp)

	if w.Status != string(domain.StatusPending) {
		t.Errorf("expected status 'pending', got %q", w.Status)
	}
	if w.Amount == "" {
		t.Error("expected amount to be set")
	}
	if w.ID.String() == "" {
		t.Error("expected ID to be set")
	}

	getResp := makeRequest(t, http.MethodGet, "/v1/withdrawals/"+w.ID.String(), nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d", getResp.StatusCode)
	}

	fetched := parseResponse[controller.WithdrawalResponse](t, getResp)
	if fetched.ID != w.ID {
		t.Errorf("expected fetched ID %s, got %s", w.ID, fetched.ID)
	}
}

func TestCreateWithdrawalInsufficientBalance(t *testing.T) {
	setupDB(t)

	payload := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "99999",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-insufficient-1",
	}

	resp := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)

	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 409, got %d, body: %s", resp.StatusCode, string(body))
	}
}

func TestCreateWithdrawalInvalidAmount(t *testing.T) {
	setupDB(t)

	tests := []struct {
		name   string
		amount string
	}{
		{"zero amount", "0"},
		{"negative amount", "-100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]string{
				"user_id":         "00000000-0000-0000-0000-000000000001",
				"amount":          tt.amount,
				"currency":        "USDT",
				"destination":     "TRx1234567890abcdef",
				"idempotency_key": "test-invalid-" + tt.amount,
			}

			resp := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)

			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status 400, got %d, body: %s", resp.StatusCode, string(body))
			}
		})
	}
}

func TestIdempotencySamePayload(t *testing.T) {
	setupDB(t)

	payload := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "25",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-idempotent-1",
	}

	resp1 := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)
	if resp1.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first request: expected 201, got %d, body: %s", resp1.StatusCode, string(body))
	}
	w1 := parseResponse[controller.WithdrawalResponse](t, resp1)

	resp2 := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)
	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second request: expected 201, got %d, body: %s", resp2.StatusCode, string(body))
	}
	w2 := parseResponse[controller.WithdrawalResponse](t, resp2)

	if w1.ID != w2.ID {
		t.Errorf("idempotent requests returned different IDs: %s vs %s", w1.ID, w2.ID)
	}
}

func TestIdempotencyDifferentPayload(t *testing.T) {
	setupDB(t)

	payload1 := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "30",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-conflict-1",
	}
	resp1 := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload1)
	if resp1.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first request: expected 201, got %d, body: %s", resp1.StatusCode, string(body))
	}
	resp1.Body.Close()

	payload2 := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "50",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-conflict-1",
	}
	resp2 := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload2)
	if resp2.StatusCode != http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("second request: expected 422, got %d, body: %s", resp2.StatusCode, string(body))
	}
	resp2.Body.Close()
}

func TestConcurrentWithdrawals(t *testing.T) {
	setupDB(t)

	const (
		numWorkers        = 20
		amountPerWithdraw = "100"
	)

	var (
		successCount int64
		failCount    int64
		wg           sync.WaitGroup
	)

	ready := make(chan struct{})

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			payload := map[string]string{
				"user_id":         "00000000-0000-0000-0000-000000000001",
				"amount":          amountPerWithdraw,
				"currency":        "USDT",
				"destination":     "TRx1234567890abcdef",
				"idempotency_key": fmt.Sprintf("concurrent-test-%d", idx),
			}

			<-ready

			resp := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)
			defer resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusCreated:
				atomic.AddInt64(&successCount, 1)
			case http.StatusConflict:
				atomic.AddInt64(&failCount, 1)
			default:
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("unexpected status %d for worker %d: %s", resp.StatusCode, idx, string(body))
			}
		}(i)
	}

	close(ready)
	wg.Wait()

	t.Logf("Results: %d successes, %d failures (insufficient balance)", successCount, failCount)

	if successCount != 10 {
		t.Errorf("expected exactly 10 successful withdrawals, got %d", successCount)
	}

	if successCount+failCount != int64(numWorkers) {
		t.Errorf("expected %d total responses, got %d", numWorkers, successCount+failCount)
	}
}

func TestUnauthorizedRequest(t *testing.T) {
	setupDB(t)

	req, err := http.NewRequest(http.MethodGet, baseURL()+"/v1/withdrawals/00000000-0000-0000-0000-000000000001", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestConfirmWithdrawal(t *testing.T) {
	setupDB(t)

	payload := map[string]string{
		"user_id":         "00000000-0000-0000-0000-000000000001",
		"amount":          "10",
		"currency":        "USDT",
		"destination":     "TRx1234567890abcdef",
		"idempotency_key": "test-confirm-1",
	}

	createResp := makeRequest(t, http.MethodPost, "/v1/withdrawals", payload)
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create: expected 201, got %d, body: %s", createResp.StatusCode, string(body))
	}
	w := parseResponse[controller.WithdrawalResponse](t, createResp)

	confirmResp := makeRequest(t, http.MethodPost, "/v1/withdrawals/"+w.ID.String()+"/confirm", nil)
	if confirmResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(confirmResp.Body)
		t.Fatalf("confirm: expected 200, got %d, body: %s", confirmResp.StatusCode, string(body))
	}
	confirmed := parseResponse[controller.WithdrawalResponse](t, confirmResp)

	if confirmed.Status != string(domain.StatusConfirmed) {
		t.Errorf("expected status 'confirmed', got %q", confirmed.Status)
	}
}
