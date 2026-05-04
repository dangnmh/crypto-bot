package main

import (
	"context"
	"fmt"
	"log"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
)

func main() {
	cfg, err := config.Load("system.jsonc")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := exchange.NewClient(cfg.API.BaseURL, cfg.APIKey, cfg.APISecret, cfg.Logging)
	ctx := context.Background()

	resp, err := client.GetCtx(ctx, "/api/v1/private/order/list/history_orders", map[string]string{
		"symbol":    "AXL_USDT",
		"page_num":  "1",
		"page_size": "5",
	})
	if err != nil {
		fmt.Println("Err:", err)
		return
	}
	fmt.Println(string(resp))
}
