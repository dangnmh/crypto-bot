package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/logger"
	"crypto-bot/tools/toolconfig"

	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: go run main.go <order_id>\nExample: go run main.go 801420766351734848")
	}
	orderID := os.Args[1]
	_ = godotenv.Load()

	// Init slog for debugging
	cleanup := logger.InitLogger("debug", "dev")
	defer cleanup()

	// Load Configuration with Bitwarden fallback
	cfg, err := toolconfig.Load("configs/funding/system.jsonc")
	if err != nil {
		slog.Error("Failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	client := mexc.NewClient(httpPool, cfg.ExchangeConfig.Mexc.Future.BaseURL, cfg.ExchangeConfig.Mexc.APIKey, cfg.ExchangeConfig.Mexc.APISecret, cfg.Logging)

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
	dealPath := fmt.Sprintf("/api/v1/private/order/deal_details/%s", orderID)
	fmt.Printf("\n--- Fetching Deal Details (Fills) for %s ---\n", orderID)
	dealResp, err := client.GetCtx(context.Background(), dealPath, nil)
	if err != nil {
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
