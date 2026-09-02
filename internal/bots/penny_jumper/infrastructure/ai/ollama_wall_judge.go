package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/httpclient"

	transportlog "github.com/dangnmh/transport"
	"github.com/go-playground/validator/v10"
)

// OllamaWallJudgeConfig (also aliased for AI Proxy) configures the AI model evaluator.
type OllamaWallJudgeConfig struct {
	Endpoint      string        `json:"endpoint" validate:"required"`
	APIKey        string        `json:"api_key" validate:"required"`
	ModelName     string        `json:"model_name" validate:"required"`
	Timeout       time.Duration `json:"timeout" validate:"gt=0"`
	MinTrustScore float64       `json:"min_trust_score" validate:"gt=0,lte=1.0"`
}

// ProxyWallJudgeConfig is an alias for OllamaWallJudgeConfig.
type ProxyWallJudgeConfig = OllamaWallJudgeConfig

// Validate validates the OllamaWallJudgeConfig using go-playground/validator.
func (c *OllamaWallJudgeConfig) Validate() error {
	v := validator.New()
	if err := v.Struct(c); err != nil {
		return fmt.Errorf("invalid proxy wall judge config: %w", err)
	}
	return nil
}

// OllamaWallJudge evaluates orderbook walls and event streams using the dedicated AI Proxy endpoint.
type OllamaWallJudge struct {
	cfg        OllamaWallJudgeConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// ProxyWallJudge is an alias for OllamaWallJudge.
type ProxyWallJudge = OllamaWallJudge

// NewOllamaWallJudge creates a new OllamaWallJudge (ProxyWallJudge).
func NewOllamaWallJudge(cfg OllamaWallJudgeConfig, httpClient *http.Client, logger *slog.Logger) (*OllamaWallJudge, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	} else {
		clientCopy = http.Client{Timeout: cfg.Timeout}
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}

	rt := clientCopy.Transport
	rt = transportlog.NewTransportLog(
		rt,
		transportlog.LogOptionLogger(logger.With("component", "ProxyWallJudge")),
		transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
			OnStatus:       []int{0},
			WhiteListPaths: []string{"*"},
		}),
		transportlog.LogOptionRedactSensitive(true),
		transportlog.LogOptionRedactSensitiveKeys([]string{"x-api-key", "Authorization"}),
		transportlog.LogOptionQueryParams(true),
	)
	clientCopy.Transport = httpclient.WrapWithRequestID(rt)

	endpoint := strings.TrimRight(cfg.Endpoint, "/")

	return &OllamaWallJudge{
		cfg: OllamaWallJudgeConfig{
			Endpoint:      endpoint,
			APIKey:        cfg.APIKey,
			ModelName:     cfg.ModelName,
			Timeout:       cfg.Timeout,
			MinTrustScore: cfg.MinTrustScore,
		},
		httpClient: &clientCopy,
		logger:     logger.With("component", "ProxyWallJudge", "model", cfg.ModelName),
	}, nil
}

// NewProxyWallJudge is an alias constructor for NewOllamaWallJudge.
func NewProxyWallJudge(cfg ProxyWallJudgeConfig, httpClient *http.Client, logger *slog.Logger) (*ProxyWallJudge, error) {
	return NewOllamaWallJudge(cfg, httpClient, logger)
}

type proxyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type proxyMessagesRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []proxyMessage `json:"messages"`
}

type proxyContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

type proxyError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type proxyMessagesResponse struct {
	ID      string             `json:"id,omitempty"`
	Type    string             `json:"type,omitempty"`
	Role    string             `json:"role,omitempty"`
	Model   string             `json:"model,omitempty"`
	Content []proxyContentItem `json:"content,omitempty"`
	Error   *proxyError        `json:"error,omitempty"`
}

type judgeOutput struct {
	TrustScore float64 `json:"trust_score"`
	IsTrusted  bool    `json:"is_trusted"`
	Reason     string  `json:"reason"`
}

// JudgeWall sends wall metadata and event history to the AI Proxy endpoint for evaluation.
func (j *OllamaWallJudge) JudgeWall(ctx context.Context, wall *pjdomain.Wall, events []pjdomain.WallEvent, trades []shared.PublicTrade) (pjdomain.WallJudgeResult, error) {
	if wall == nil || len(events) == 0 {
		return pjdomain.WallJudgeResult{
			WallID:     "",
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     pjdomain.ReasonEmptyWallOrEvents,
		}, nil
	}

	metrics := pjdomain.ReconcileWallData(wall, events, trades)
	prompt := j.buildPrompt(wall, events, metrics)

	reqPayload := proxyMessagesRequest{
		Model:     j.cfg.ModelName,
		MaxTokens: 300,
		Messages: []proxyMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	bodyBytes, reason, err := j.doProxyRequest(ctx, reqPayload)
	if err != nil {
		return pjdomain.WallJudgeResult{
			WallID:     wall.ID,
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     reason,
		}, err
	}

	output, reason, isAPIError, ok := j.parseModelOutput(bodyBytes)
	if !ok {
		if isAPIError {
			return pjdomain.WallJudgeResult{
				WallID:     wall.ID,
				TrustScore: 0,
				IsTrusted:  false,
				Reason:     reason,
			}, fmt.Errorf("%s", reason)
		}
		return pjdomain.WallJudgeResult{
			WallID:     wall.ID,
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     reason,
		}, nil
	}

	isTrusted := output.IsTrusted && (output.TrustScore >= j.cfg.MinTrustScore)

	return pjdomain.WallJudgeResult{
		WallID:     wall.ID,
		TrustScore: output.TrustScore,
		IsTrusted:  isTrusted,
		Reason:     output.Reason,
	}, nil
}

func (j *OllamaWallJudge) doProxyRequest(ctx context.Context, reqPayload proxyMessagesRequest) ([]byte, string, error) {
	reqBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, "PROXY_PAYLOAD_MARSHAL_ERROR", fmt.Errorf("marshal proxy request: %w", err)
	}

	apiURL := j.cfg.Endpoint
	if !strings.HasSuffix(apiURL, "/v1/messages") {
		apiURL = fmt.Sprintf("%s/v1/messages", apiURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, "PROXY_CREATE_REQUEST_ERROR", fmt.Errorf("create proxy http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if j.cfg.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", j.cfg.APIKey))
	}

	start := time.Now()
	resp, err := j.httpClient.Do(req)
	if err != nil {
		j.logger.ErrorContext(ctx, "AI Proxy HTTP call failed", slog.Any("error", err), slog.Duration("elapsed", time.Since(start)))
		return nil, fmt.Sprintf("PROXY_CALL_ERROR: %v", err), err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "PROXY_BODY_READ_ERROR", fmt.Errorf("read proxy response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		j.logger.ErrorContext(ctx, "AI Proxy returned non-200 status",
			slog.Int("status", resp.StatusCode),
			slog.String("body", string(bodyBytes)),
		)
		return nil, fmt.Sprintf("PROXY_STATUS_%d", resp.StatusCode), fmt.Errorf("proxy http status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return bodyBytes, "", nil
}

func (j *OllamaWallJudge) parseModelOutput(bodyBytes []byte) (output judgeOutput, reason string, isAPIError, ok bool) {
	var genResp proxyMessagesResponse
	if err := json.Unmarshal(bodyBytes, &genResp); err != nil {
		return judgeOutput{}, "PROXY_INVALID_ENVELOPE_JSON", false, false
	}

	if genResp.Error != nil && genResp.Error.Message != "" {
		return judgeOutput{}, fmt.Sprintf("PROXY_API_ERROR: %s", genResp.Error.Message), true, false
	}

	rawText := extractResponseText(genResp.Content)
	if rawText == "" {
		return judgeOutput{}, "PROXY_EMPTY_RESPONSE", false, false
	}

	if err := json.Unmarshal([]byte(rawText), &output); err != nil {
		return judgeOutput{}, "PROXY_INVALID_MODEL_JSON", false, false
	}
	return output, "", false, true
}

func extractResponseText(content []proxyContentItem) string {
	var rawText string
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			rawText = item.Text
			break
		}
	}
	if rawText == "" && len(content) > 0 {
		for _, item := range content {
			if item.Text != "" {
				rawText = item.Text
				break
			}
		}
	}
	rawText = strings.TrimSpace(rawText)
	rawText = strings.TrimPrefix(rawText, "```json")
	rawText = strings.TrimPrefix(rawText, "```")
	rawText = strings.TrimSuffix(rawText, "```")
	return strings.TrimSpace(rawText)
}

type compactWallEvent struct {
	Offset        string  `json:"offset"`
	Type          string  `json:"type"`
	Volume        float64 `json:"volume"`
	DeltaVolume   float64 `json:"delta,omitempty"`
	DistancePct   float64 `json:"dist_pct"`
	RelativeRatio float64 `json:"ratio_x"`
}

func simplifyEvents(birth time.Time, events []pjdomain.WallEvent) []compactWallEvent {
	const maxEvents = 15
	if len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}

	simplified := make([]compactWallEvent, 0, len(events))
	for _, e := range events {
		offset := 0.0
		if !birth.IsZero() && !e.Timestamp.IsZero() {
			offset = e.Timestamp.Sub(birth).Seconds()
			if offset < 0 {
				offset = 0
			}
		}
		simplified = append(simplified, compactWallEvent{
			Offset:        fmt.Sprintf("+%.2fs", offset),
			Type:          string(e.EventType),
			Volume:        e.Volume,
			DeltaVolume:   e.DeltaVolume,
			DistancePct:   e.DistancePct,
			RelativeRatio: e.RelativeRatio,
		})
	}
	return simplified
}

func (j *OllamaWallJudge) buildPrompt(wall *pjdomain.Wall, events []pjdomain.WallEvent, metrics pjdomain.WallMetrics) string {
	simplifiedEvents := simplifyEvents(wall.FirstDetectedAt, events)
	eventsJSON, _ := json.Marshal(simplifiedEvents)

	ageSec := wall.GetAge().Seconds()

	return fmt.Sprintf(`You are a quantitative cryptocurrency microstructure and orderbook wall analyst.
Evaluate whether the following detected orderbook wall is genuine structural liquidity or algorithmic spoofing.

Wall Summary:
- Exchange: %s | Symbol: %s | Side: %s
- Price: %f | Age: %.2fs
- Initial Volume: %f -> Current Volume: %f
- Verified Absorbed Volume: %f (filled by taker trades)
- Pulled Volume: %f (canceled without fills)
- Relative Size vs Nearby Depth: %.2fx
- Distance from BBO: %.2f%%%%
- OrderBook Depth Imbalance (1%% BBO): %.2fx
- Backing Support Depth Behind Wall: %.2fx
- 1h History at Price Level: %d Pulls / %d Fills
- Volume 24h: %.2f USDT | 1m Turnover Multiple: %.1fx

Recent Event Journal:
%s

Analysis Rules:
1. Genuine walls accept taker fills (Verified Absorbed Volume > 0).
2. Walls surviving > 3s without pulling have passed flash-spoof filters.
3. Walls that constantly flicker/resize without taker fills or pull when price approaches are predatory spoofs.
4. Favorable orderbook depth imbalance (> 1.5x) and multi-level backing (> 0.8x) provide strong structural support.
5. Unfavorable depth imbalance (< 0.8x) or air-pocket backing (< 0.3x) carry high breakdown risk.
6. Price levels with recurring pulls (>= 3 Pulls and 0 Fills in 1h) are algorithmic spoof traps.
7. Price levels with verified executions (> 0 Fills) indicate genuine market maker limit liquidity.
8. Output a trust score between 0.00 and 1.00. Score >= %.2f is trusted.

Return ONLY a valid JSON object in this exact schema:
{
  "trust_score": <float between 0.0 and 1.0>,
  "is_trusted": <boolean>,
  "reason": "<short concise uppercase explanation, e.g. GENUINE_ABSORPTION_20PCT or FLICKER_SPOOF>"
}`,
		wall.Exchange,
		wall.Symbol,
		wall.Side.String(),
		wall.Price,
		ageSec,
		wall.InitialVolume,
		wall.Volume,
		metrics.AbsorbedVolume,
		metrics.PulledVolume,
		wall.RelativeRatio,
		wall.DistancePct,
		wall.DepthImbalance,
		wall.BackingRatio,
		wall.PullCount1h,
		wall.FillCount1h,
		wall.Vol24h,
		wall.WallTo1mRatio,
		string(eventsJSON),
		j.cfg.MinTrustScore,
	)
}
