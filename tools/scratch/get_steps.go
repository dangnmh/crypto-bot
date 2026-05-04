package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run get_steps.go <symbol>")
		fmt.Println("Example: go run get_steps.go PEPE_USDT")
		return
	}

	symbol := os.Args[1]
	url := fmt.Sprintf("https://contract.mexc.com/api/v1/contract/detail?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Lỗi gọi API: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Lỗi đọc response: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		log.Fatalf("Lỗi parse JSON: %v", err)
	}

	if success, ok := data["success"].(bool); !ok || !success {
		log.Fatalf("API báo lỗi: %v", string(body))
	}

	// data["data"] is usually a struct for the contract
	detail, ok := data["data"].(map[string]interface{})
	if !ok {
		log.Fatalf("Không tìm thấy thông tin contract")
	}

	if depthSteps, ok := detail["depthStepList"].([]interface{}); ok {
		fmt.Printf("✅ Các mức gộp sổ lệnh (obStep) hợp lệ cho %s:\n", symbol)
		for _, step := range depthSteps {
			fmt.Printf("   - \"%v\"\n", step)
		}
		fmt.Println("\n💡 Tip: Hãy copy một trong các giá trị trong ngoặc kép ở trên điền vào \"obStep\" trong file funding.jsonc.")
	} else {
		fmt.Println("❌ Không tìm thấy thông tin depthStepList. Có thể API đổi format hoặc symbol không đúng.")
	}
}
