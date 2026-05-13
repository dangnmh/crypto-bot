package main

import (
	"context"
	"fmt"
	"log"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/httpclient"
)

func main() {
	cfg, err := pkgconfig.Load[sysconfig.SystemConfig]("configs/funding/system.jsonc")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	httpPool := httpclient.NewPool(httpclient.DefaultPoolConfig())
	client := mexc.NewClient(httpPool, cfg.API.Future.BaseURL, cfg.APIKey, cfg.APISecret, cfg.Logging)
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
