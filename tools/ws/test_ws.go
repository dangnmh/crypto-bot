package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := "wss://contract.mexc.com/edge"
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer func() { _ = c.Close() }()

	msg := map[string]interface{}{
		"method": "sub.depth.full",
		"param": map[string]interface{}{
			"symbol": "BTC_USDT",
			"limit":  20,
		},
	}
	_ = c.WriteJSON(msg)

	for i := 0; i < 5; i++ {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		var decoded map[string]interface{}
		_ = json.Unmarshal(message, &decoded)
		if channel, ok := decoded["channel"]; ok && channel == "push.depth.full" {
			data := decoded["data"].(map[string]interface{})
			fmt.Printf("Received depth full: %d asks, %d bids\n", len(data["asks"].([]interface{})), len(data["bids"].([]interface{})))
		} else {
			fmt.Printf("recv: %s\n", message)
		}
	}
}
