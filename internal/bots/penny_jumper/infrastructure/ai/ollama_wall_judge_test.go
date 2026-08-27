package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/ai"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaWallJudge_ValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing Endpoint returns validation error", func(t *testing.T) {
		t.Parallel()
		_, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "",
			APIKey:        "sk-test",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       2 * time.Second,
			MinTrustScore: 0.70,
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Endpoint")
	})

	t.Run("missing APIKey returns validation error", func(t *testing.T) {
		t.Parallel()
		_, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "http://localhost:8317",
			APIKey:        "",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       2 * time.Second,
			MinTrustScore: 0.70,
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "APIKey")
	})

	t.Run("missing ModelName returns validation error", func(t *testing.T) {
		t.Parallel()
		_, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "http://localhost:8317",
			APIKey:        "sk-test",
			ModelName:     "",
			Timeout:       2 * time.Second,
			MinTrustScore: 0.70,
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ModelName")
	})

	t.Run("invalid MinTrustScore returns validation error", func(t *testing.T) {
		t.Parallel()
		_, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "http://localhost:8317",
			APIKey:        "sk-test",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       2 * time.Second,
			MinTrustScore: 1.5,
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MinTrustScore")
	})

	t.Run("zero Timeout returns validation error", func(t *testing.T) {
		t.Parallel()
		_, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "http://localhost:8317",
			APIKey:        "sk-test",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       0,
			MinTrustScore: 0.70,
		}, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Timeout")
	})
}

func TestOllamaWallJudge_EmptyInputs(t *testing.T) {
	t.Parallel()

	judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
		Endpoint:      "http://localhost:8317",
		APIKey:        "sk-test",
		ModelName:     "gemini-3.7-flash-high",
		Timeout:       2 * time.Second,
		MinTrustScore: 0.70,
	}, nil, nil)
	require.NoError(t, err)

	ctx := context.Background()
	res, err := judge.JudgeWall(ctx, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, pjdomain.ReasonEmptyWallOrEvents, res.Reason)
	assert.False(t, res.IsTrusted)

	wall := &pjdomain.Wall{ID: "w-1"}
	res, err = judge.JudgeWall(ctx, wall, []pjdomain.WallEvent{}, nil)
	require.NoError(t, err)
	assert.Equal(t, pjdomain.ReasonEmptyWallOrEvents, res.Reason)
	assert.False(t, res.IsTrusted)
}

func TestOllamaWallJudge_SuccessfulEvaluation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer sk-test-key", r.Header.Get("Authorization"))

		var reqBody map[string]any
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		assert.NoError(t, err)
		assert.Equal(t, "gemini-3.7-flash-high", reqBody["model"])
		messages, ok := reqBody["messages"].([]any)
		assert.True(t, ok)
		assert.NotEmpty(t, messages)

		resp := map[string]any{
			"id":    "msg_123",
			"type":  "message",
			"role":  "assistant",
			"model": "gemini-3.7-flash-high",
			"content": []map[string]any{
				{
					"type":     "thinking",
					"thinking": "Analyzing wall metrics...",
				},
				{
					"type": "text",
					"text": `{"trust_score": 0.88, "is_trusted": true, "reason": "GENUINE_ABSORPTION"}`,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
		Endpoint:      server.URL,
		APIKey:        "sk-test-key",
		ModelName:     "gemini-3.7-flash-high",
		Timeout:       2 * time.Second,
		MinTrustScore: 0.70,
	}, server.Client(), nil)
	require.NoError(t, err)

	now := time.Now()
	wall := &pjdomain.Wall{
		ID:              "wall-123",
		Exchange:        "mexc_spot",
		Symbol:          "BTC_USDT",
		Side:            shared.SideOpenLong,
		Price:           95000.0,
		InitialVolume:   10.0,
		Volume:          8.5,
		RelativeRatio:   25.0,
		FirstDetectedAt: now.Add(-5 * time.Second),
	}

	events := []pjdomain.WallEvent{
		{
			WallID:        "wall-123",
			EventType:     pjdomain.WallEventBorn,
			Volume:        10.0,
			RelativeRatio: 25.0,
			DistancePct:   0.3,
			SpreadPct:     0.05,
			Timestamp:     now.Add(-5 * time.Second),
		},
		{
			WallID:        "wall-123",
			EventType:     pjdomain.WallEventResized,
			Volume:        8.5,
			DeltaVolume:   -1.5,
			RelativeRatio: 21.25,
			DistancePct:   0.2,
			SpreadPct:     0.05,
			Timestamp:     now,
		},
	}

	trades := []shared.PublicTrade{
		{
			Symbol:    "BTCUSDT",
			Price:     60000.0,
			Volume:    1.5,
			Side:      shared.SideOpenShort,
			Timestamp: now,
		},
	}

	ctx := context.Background()
	res, err := judge.JudgeWall(ctx, wall, events, trades)
	require.NoError(t, err)
	assert.Equal(t, "wall-123", res.WallID)
	assert.InDelta(t, 0.88, res.TrustScore, 0.001)
	assert.True(t, res.IsTrusted)
	assert.Equal(t, "GENUINE_ABSORPTION", res.Reason)
}

func TestOllamaWallJudge_MarkdownWrappedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_456",
			"type":  "message",
			"role":  "assistant",
			"model": "gemini-3.7-flash-high",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "```json\n{\"trust_score\": 0.65, \"is_trusted\": false, \"reason\": \"PULL_RISK\"}\n```",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
		Endpoint:      server.URL,
		APIKey:        "sk-test-key",
		ModelName:     "gemini-3.7-flash-high",
		Timeout:       2 * time.Second,
		MinTrustScore: 0.70,
	}, server.Client(), nil)
	require.NoError(t, err)

	wall := &pjdomain.Wall{ID: "w-markdown", Price: 100}
	events := []pjdomain.WallEvent{{WallID: "w-markdown", Timestamp: time.Now()}}

	res, err := judge.JudgeWall(context.Background(), wall, events, nil)
	require.NoError(t, err)
	assert.InDelta(t, 0.65, res.TrustScore, 0.001)
	assert.False(t, res.IsTrusted)
	assert.Equal(t, "PULL_RISK", res.Reason)
}

func TestOllamaWallJudge_Errors(t *testing.T) {
	t.Parallel()

	t.Run("HTTP 500 status returns error", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer server.Close()

		judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      server.URL,
			APIKey:        "sk-test",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       2 * time.Second,
			MinTrustScore: 0.70,
		}, server.Client(), nil)
		require.NoError(t, err)

		wall := &pjdomain.Wall{ID: "w-err"}
		events := []pjdomain.WallEvent{{WallID: "w-err", Timestamp: time.Now()}}

		res, err := judge.JudgeWall(context.Background(), wall, events, nil)
		require.Error(t, err)
		assert.Contains(t, res.Reason, "PROXY_STATUS_500")
		assert.False(t, res.IsTrusted)
	})

	t.Run("Proxy API error response", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"error": map[string]any{
					"type":    "invalid_request_error",
					"message": "model 'non-existent' not found",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      server.URL,
			APIKey:        "sk-test",
			ModelName:     "non-existent",
			Timeout:       2 * time.Second,
			MinTrustScore: 0.70,
		}, server.Client(), nil)
		require.NoError(t, err)

		wall := &pjdomain.Wall{ID: "w-err"}
		events := []pjdomain.WallEvent{{WallID: "w-err", Timestamp: time.Now()}}

		res, err := judge.JudgeWall(context.Background(), wall, events, nil)
		require.Error(t, err)
		assert.Contains(t, res.Reason, "PROXY_API_ERROR")
	})

	t.Run("malformed response variations", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			rawResponse string
			wantReason  string
		}{
			{
				name:        "invalid inner json",
				rawResponse: "this is not valid json",
				wantReason:  "PROXY_INVALID_MODEL_JSON",
			},
			{
				name:        "empty response text",
				rawResponse: "",
				wantReason:  "PROXY_EMPTY_RESPONSE",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					resp := map[string]any{
						"id":    "msg_test",
						"type":  "message",
						"role":  "assistant",
						"model": "gemini-3.7-flash-high",
						"content": []map[string]any{
							{
								"type": "text",
								"text": tc.rawResponse,
							},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(resp)
				}))
				defer srv.Close()

				j, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
					Endpoint:      srv.URL,
					APIKey:        "sk-test",
					ModelName:     "gemini-3.7-flash-high",
					Timeout:       2 * time.Second,
					MinTrustScore: 0.70,
				}, srv.Client(), nil)
				require.NoError(t, err)

				wall := &pjdomain.Wall{ID: "w-malformed"}
				events := []pjdomain.WallEvent{{WallID: "w-malformed", Timestamp: time.Now()}}

				evalRes, evalErr := j.JudgeWall(context.Background(), wall, events, nil)
				require.NoError(t, evalErr)
				assert.Equal(t, tc.wantReason, evalRes.Reason)
			})
		}
	})

	t.Run("network connection failure", func(t *testing.T) {
		t.Parallel()
		judge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      "http://127.0.0.1:54321", // non-existent port
			APIKey:        "sk-test",
			ModelName:     "gemini-3.7-flash-high",
			Timeout:       100 * time.Millisecond,
			MinTrustScore: 0.70,
		}, nil, nil)
		require.NoError(t, err)

		wall := &pjdomain.Wall{ID: "w-err"}
		events := []pjdomain.WallEvent{{WallID: "w-err", Timestamp: time.Now()}}

		res, err := judge.JudgeWall(context.Background(), wall, events, nil)
		require.Error(t, err)
		assert.Contains(t, res.Reason, "PROXY_CALL_ERROR")
	})
}
