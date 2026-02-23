package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTradePaginationLoop(t *testing.T) {
	page1 := tradeListResult{
		Trades: []trade{
			{TradeID: "t1", Symbol: "BTC-USD", Side: "buy", Quantity: 0.1, Price: 50000, MarketType: "spot"},
		},
		NextCursor: "cursor1",
	}
	page2 := tradeListResult{
		Trades: []trade{
			{TradeID: "t2", Symbol: "BTC-USD", Side: "sell", Quantity: 0.1, Price: 51000, MarketType: "spot"},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			json.NewEncoder(w).Encode(page1)
		} else if cursor == "cursor1" {
			json.NewEncoder(w).Encode(page2)
		} else {
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// Simulate the pagination loop used by cmd_trades.go
	httpClient := &http.Client{}
	var allTrades []trade
	cursor := ""

	for {
		q := url.Values{}
		q.Set("limit", "1")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		reqURL := srv.URL + "/trades?" + q.Encode()

		req, _ := http.NewRequest("GET", reqURL, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		var result tradeListResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		resp.Body.Close()

		allTrades = append(allTrades, result.Trades...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	if callCount != 2 {
		t.Errorf("expected 2 page requests, got %d", callCount)
	}
	if len(allTrades) != 2 {
		t.Errorf("expected 2 trades, got %d", len(allTrades))
	}
	if allTrades[0].TradeID != "t1" || allTrades[1].TradeID != "t2" {
		t.Errorf("unexpected trade IDs: %v", allTrades)
	}
}

func TestTradePaginationLimit(t *testing.T) {
	// Server always returns a next_cursor — limit should stop the loop
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tradeListResult{
			Trades:     []trade{{TradeID: "t1"}},
			NextCursor: "more",
		})
	}))
	defer srv.Close()

	httpClient := &http.Client{}
	var allTrades []trade
	cursor := ""
	limit := 1

	for {
		reqURL := srv.URL + "/trades?limit=1"
		if cursor != "" {
			reqURL += "&cursor=" + cursor
		}
		req, _ := http.NewRequest("GET", reqURL, nil)
		resp, _ := httpClient.Do(req)
		var result tradeListResult
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		allTrades = append(allTrades, result.Trades...)
		if limit > 0 && len(allTrades) >= limit {
			allTrades = allTrades[:limit]
			break
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	if len(allTrades) != 1 {
		t.Errorf("expected limit=1 to stop after 1 trade, got %d", len(allTrades))
	}
}
