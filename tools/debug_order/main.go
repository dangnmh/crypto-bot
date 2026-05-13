package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/logger"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run main.go <order_id>\nExample: go run main.go 801420766351734848")
	}
	orderID := os.Args[1]

	// Init slog for debugging
	cleanup := logger.InitLogger("debug")
	defer cleanup()

	// Load Configuration relative to project root
	cfg, err := pkgconfig.Load[sysconfig.SystemConfig]("configs/funding/system.jsonc")
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	client := mexc.NewClient(httpPool, cfg.API.Future.BaseURL, cfg.APIKey, cfg.APISecret, cfg.Logging)

	// 1. Get Order Details
	orderPath := fmt.Sprintf("/api/v1/private/order/get/%s", orderID)
	fmt.Printf("\n--- Fetching Order Details for %s ---\n", orderID)
	orderResp, err := client.GetCtx(context.Background(), orderPath, nil)
	if err != nil {
		log.Printf("Failed to get order details: %v", err)
	} else {
		prettyPrint(orderResp)
	}

	// 2. Get Order Fills/Deals (Trades)
	// Some exchanges put fills into a separate endpoint.
	// We'll query /api/v1/private/deal/list/order_id or related endpoint if order check alone doesn't have createTime
	// Note: MEXC /api/v1/private/deal/list/order_id might not exist, usually it's /private/deal/list or similar,
	// but let's query the specific order's trades.
	dealPath := fmt.Sprintf("/api/v1/private/order/deal_details/%s", orderID)
	fmt.Printf("\n--- Fetching Deal Details (Fills) for %s ---\n", orderID)
	dealResp, err := client.GetCtx(context.Background(), dealPath, nil)
	if err != nil {
		// Just a fallback since MEXC endpoint documentation varies.
		log.Printf("Failed to get deal details (might be normal if endpoint is different): %v", err)
	} else {
		prettyPrint(dealResp)
	}
}

func prettyPrint(data []byte) {
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err == nil {
		fmt.Println(out.String())
	} else {
		fmt.Println(string(data))
	}
}
