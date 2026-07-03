package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gorilla/websocket"

	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/eventbus"
)

// Helper function to capitalize and make names camel-case compatible for Go struct fields.
func toGoPublicName(s string) string {
	if s == "" {
		return "Empty"
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "/", "_")
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	name := strings.Join(parts, "")
	name = strings.ReplaceAll(name, "Id", "ID")
	name = strings.ReplaceAll(name, "Api", "API")
	name = strings.ReplaceAll(name, "Url", "URL")
	name = strings.ReplaceAll(name, "Pnl", "PNL")
	name = strings.ReplaceAll(name, "Tp", "TP")
	name = strings.ReplaceAll(name, "Sl", "SL")
	name = strings.ReplaceAll(name, "Ws", "WS")
	return name
}

type structField struct {
	Name string
	Type string
	Tag  string
}

type generator struct {
	structs map[string]string
}

func newGenerator() *generator {
	return &generator{
		structs: make(map[string]string),
	}
}

func (g *generator) generate(name string, val any) string {
	typeName := toGoPublicName(name)
	switch v := val.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		fields := make([]structField, 0, len(keys))
		for _, k := range keys {
			valItem := v[k]
			fieldName := toGoPublicName(k)
			var fieldType string
			tag := fmt.Sprintf("`json:\"%s\"`", k)

			switch item := valItem.(type) {
			case map[string]any:
				subStructName := typeName + fieldName
				g.generate(subStructName, item)
				fieldType = subStructName
			case []any:
				if len(item) > 0 {
					first := item[0]
					switch firstVal := first.(type) {
					case map[string]any:
						subStructName := typeName + fieldName + "Item"
						g.generate(subStructName, firstVal)
						fieldType = "[]" + subStructName
					default:
						fieldType = "[]" + g.primitiveType(firstVal)
					}
				} else {
					fieldType = "[]any"
				}
			default:
				fieldType = g.primitiveType(item)
			}
			fields = append(fields, structField{
				Name: fieldName,
				Type: fieldType,
				Tag:  tag,
			})
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "type %s struct {\n", typeName)
		for _, f := range fields {
			fmt.Fprintf(&sb, "\t%s %s %s\n", f.Name, f.Type, f.Tag)
		}
		sb.WriteString("}\n")
		g.structs[typeName] = sb.String()
		return typeName

	case []any:
		if len(v) > 0 {
			return "[]" + g.generate(typeName+"Item", v[0])
		}
		return "[]any"
	default:
		return g.primitiveType(v)
	}
}

func (g *generator) primitiveType(val any) string {
	if val == nil {
		return "any"
	}
	switch v := val.(type) {
	case string:
		return "xjson.Number"
	case float64:
		if v == float64(int64(v)) {
			return "int64"
		}
		return "xjson.Number"
	case bool:
		return "bool"
	default:
		return reflect.TypeOf(val).String()
	}
}

func (g *generator) output() string {
	keys := make([]string, 0, len(g.structs))
	for k := range g.structs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(g.structs[k])
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseRESTArgs() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run tools/spec_helper/main.go rest <url> [http_method] [payload_json]")
		os.Exit(1)
	}
	url := os.Args[2]
	method := "GET"
	if len(os.Args) >= 4 {
		method = strings.ToUpper(os.Args[3])
	}
	var payload io.Reader
	if len(os.Args) >= 5 {
		payload = bytes.NewBufferString(os.Args[4])
	}
	runREST(method, url, payload)
}

func parseWSArgs() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run tools/spec_helper/main.go ws <ws_url> [subscribe_payload_json]")
		os.Exit(1)
	}
	url := os.Args[2]
	var subPayload string
	if len(os.Args) >= 4 {
		subPayload = os.Args[3]
	}
	runWS(url, subPayload)
}

func parsePrivateRESTArgs() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run tools/spec_helper/main.go private-rest <exchange> <method> [param_key=val param_key2=val2 ...]")
		os.Exit(1)
	}
	exchangeName := os.Args[2]
	method := os.Args[3]
	params := make(map[string]string)
	for i := 4; i < len(os.Args); i++ {
		parts := strings.SplitN(os.Args[i], "=", 2)
		if len(parts) == 2 {
			params[parts[0]] = parts[1]
		}
	}
	runPrivateREST(exchangeName, method, params)
}

func parsePrivateWSArgs() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run tools/spec_helper/main.go private-ws <exchange> <channel_name>")
		os.Exit(1)
	}
	exchangeName := os.Args[2]
	channelName := os.Args[3]
	runPrivateWS(exchangeName, channelName)
}

func parseJSONArgs() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run tools/spec_helper/main.go json <json_string_or_filepath>")
		os.Exit(1)
	}
	arg := os.Args[2]
	var content []byte
	var err error
	if _, errStat := os.Stat(arg); errStat == nil {
		content, err = os.ReadFile(arg)
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			os.Exit(1)
		}
	} else {
		content = []byte(arg)
	}
	generateFromJSON(content)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	mode := os.Args[1]
	switch mode {
	case "rest":
		parseRESTArgs()
	case "ws":
		parseWSArgs()
	case "private-rest":
		parsePrivateRESTArgs()
	case "private-ws":
		parsePrivateWSArgs()
	case "json":
		parseJSONArgs()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Crypto-Bot API & WS Spec Helper")
	fmt.Println("Modes:")
	fmt.Println("  rest:         Fetch public REST endpoint and generate Go structs")
	fmt.Println("                go run tools/spec_helper/main.go rest <url> [method] [payload]")
	fmt.Println("  ws:           Connect to public WS, subscribe, and generate Go structs")
	fmt.Println("                go run tools/spec_helper/main.go ws <ws_url> [sub_payload_json]")
	fmt.Println("  private-rest: Fetch private REST endpoint using configured exchange credentials")
	fmt.Println("                go run tools/spec_helper/main.go private-rest <exchange> <method> [key=val ...]")
	fmt.Println("  private-ws:   Connect to private WS, authenticate, subscribe, and generate Go structs")
	fmt.Println("                go run tools/spec_helper/main.go private-ws <exchange> <channel_name>")
	fmt.Println("  json:         Generate Go structs from a raw JSON string or JSON file")
	fmt.Println("                go run tools/spec_helper/main.go json <json_string_or_filepath>")
}

func runREST(method, url string, payload io.Reader) {
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}

	fmt.Println("--- RAW JSON RESPONSE ---")
	var pretty bytes.Buffer
	if errJson := json.Indent(&pretty, body, "", "  "); errJson == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(body))
	}
	fmt.Println("-------------------------")
	fmt.Println()

	generateFromJSON(body)
}

func connectAndSubscribe(wsURL, subPayload string) (*websocket.Conn, error) {
	fmt.Printf("Connecting to %s...\n", wsURL)
	dialer := websocket.DefaultDialer
	c, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("WebSocket dial failed: %w", err)
	}

	if subPayload != "" {
		fmt.Printf("Sending subscription payload: %s\n", subPayload)
		var msg any
		var errWrite error
		if errJson := json.Unmarshal([]byte(subPayload), &msg); errJson != nil {
			fmt.Printf("Failed to parse sub_payload as JSON: %v. Sending raw string.\n", errJson)
			errWrite = c.WriteMessage(websocket.TextMessage, []byte(subPayload))
		} else {
			errWrite = c.WriteJSON(msg)
		}
		if errWrite != nil {
			_ = c.Close()
			return nil, fmt.Errorf("failed to write subscription message: %w", errWrite)
		}
	}
	return c, nil
}

func processWSMessage(message []byte) {
	fmt.Println("--- RAW WS MESSAGE RECEIVED ---")
	var pretty bytes.Buffer
	if errJson := json.Indent(&pretty, message, "", "  "); errJson == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(message))
	}
	fmt.Println("--------------------------------")
	fmt.Println()

	generateFromJSON(message)
}

func isPingPongOrEmpty(message []byte) bool {
	var testMsg map[string]any
	if errJson := json.Unmarshal(message, &testMsg); errJson == nil {
		if len(testMsg) <= 1 {
			if _, hasPing := testMsg["ping"]; hasPing {
				return true
			}
			if _, hasPong := testMsg["pong"]; hasPong {
				return true
			}
		}
	}
	return false
}

func readWSMessages(c *websocket.Conn) {
	fmt.Println("Listening for messages (waiting for first data message)...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Timeout waiting for WebSocket message")
			return
		default:
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, message, err := c.ReadMessage()
			if err != nil {
				fmt.Printf("WebSocket read error: %v\n", err)
				return
			}

			if isPingPongOrEmpty(message) {
				continue
			}

			processWSMessage(message)
			return
		}
	}
}

func runWS(wsURL, subPayload string) {
	c, err := connectAndSubscribe(wsURL, subPayload)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()

	readWSMessages(c)
}

func generateFromJSON(data []byte) {
	var val any
	if err := json.Unmarshal(data, &val); err != nil {
		fmt.Printf("Failed to parse response as JSON: %v\n", err)
		return
	}

	g := newGenerator()
	g.generate("AutoGenerated", val)

	fmt.Println("--- AUTO-GENERATED GOLANG STRUCTS ---")
	fmt.Println("// Remember to import \"crypto-bot/pkg/xjson\" if using xjson.Number")
	fmt.Println(g.output())
	fmt.Println("-------------------------------------")
}

func loadConfigForExchange(exchangeName string) (*sysconfig.SystemConfig, error) {
	cfg, err := pkgconfig.Load[sysconfig.SystemConfig]("configs/funding/local/system.jsonc")
	if err != nil {
		return nil, err
	}
	exchCfg, err := pkgconfig.Load[sysconfig.SystemConfig]("configs/funding/local/exchange.jsonc")
	if err != nil {
		return nil, err
	}
	cfg.ExchangeConfig = exchCfg.ExchangeConfig

	// Force enable the target exchange so InitializeBase processes its credentials
	apiCfg := cfg.ExchangeConfig[exchangeName]
	apiCfg.Enable = true
	cfg.ExchangeConfig[exchangeName] = apiCfg

	if err := sysconfig.InitializeBase(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func getProvider(ctx context.Context, cfg *sysconfig.SystemConfig, exchangeName string) (*app.ExchangeProvider, error) {
	factories := app.DefaultProviderFactories()
	for _, f := range factories {
		if f.Name() == exchangeName {
			prov, err := f.Build(ctx, app.ProviderFactoryConfig{
				SystemConfig:     cfg,
				HTTPClient:       http.DefaultClient,
				Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
				Bus:              eventbus.New(slog.Default()),
				TimeSyncInterval: 30 * time.Second,
			})
			return prov, err
		}
	}
	return nil, fmt.Errorf("provider factory not found for exchange: %s", exchangeName)
}

func runPrivateREST(exchangeName, method string, params map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := loadConfigForExchange(exchangeName)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	provider, err := getProvider(ctx, cfg, exchangeName)
	if err != nil {
		fmt.Printf("Error getting provider: %v\n", err)
		return
	}

	rawReq, ok := provider.Client.(exchange.RawRequest)
	if !ok {
		fmt.Println("Error: client does not implement exchange.RawRequest interface")
		return
	}

	var data []byte
	switch strings.ToLower(method) {
	case "open_positions":
		data, err = rawReq.GetOpenPositionsRaw(ctx, params)
	case "history_positions":
		data, err = rawReq.GetHistoryPositionsRaw(ctx, params)
	case "order_detail":
		orderID := params["order_id"]
		delete(params, "order_id")
		data, err = rawReq.GetOrderDetailRaw(ctx, orderID, params)
	case "history_orders":
		data, err = rawReq.GetHistoryOrdersRaw(ctx, params)
	case "order_pnl":
		data, err = rawReq.GetOrderPNLRaw(ctx, params)
	case "funding_rate":
		data, err = rawReq.GetFundingRateRaw(ctx, params)
	case "tickers":
		data, err = rawReq.GetTickersRaw(ctx, params)
	default:
		fmt.Printf("Unsupported raw REST method: %s\n", method)
		return
	}

	if err != nil {
		fmt.Printf("API request failed: %v\n", err)
		return
	}

	fmt.Println("--- RAW JSON RESPONSE ---")
	var pretty bytes.Buffer
	if errJson := json.Indent(&pretty, data, "", "  "); errJson == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(data))
	}
	fmt.Println("-------------------------")
	fmt.Println()

	generateFromJSON(data)
}

func runPrivateWS(exchangeName, channelName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg, err := loadConfigForExchange(exchangeName)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	provider, err := getProvider(ctx, cfg, exchangeName)
	if err != nil {
		fmt.Printf("Error getting provider: %v\n", err)
		return
	}

	// Connect the WS client/pool
	fmt.Printf("Connecting WebSocket for %s...\n", exchangeName)
	provider.WS.Connect(ctx)

	resolvedChannel := channelName
	switch strings.ToLower(channelName) {
	case "position", "positions":
		resolvedChannel = "personal.position"
	case "order", "orders":
		resolvedChannel = "personal.order"
	}

	fmt.Printf("Listening for authenticated events on channel: %s\n", resolvedChannel)
	msgChan := make(chan []byte, 10)
	provider.WS.On(resolvedChannel, func(data []byte) {
		msgChan <- data
	})

	select {
	case data := <-msgChan:
		fmt.Printf("Received message on channel %s:\n", resolvedChannel)
		processWSMessage(data)
	case <-ctx.Done():
		fmt.Println("Timeout waiting for WebSocket message")
	}
}
