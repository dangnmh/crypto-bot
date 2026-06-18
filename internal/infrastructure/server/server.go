package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

const (
	errKey                   = "error"
	errRawRequestNotSupport  = "Client does not support RawRequest interface"
	errOrderIDRequired       = "order ID is required"
	errProviderNotFound      = "provider not found in context"
	errInvalidProviderType   = "invalid provider type"
	errClosedPnLNotSupported = "Closed PnL retrieval not supported for this exchange"
)

type APIServer struct {
	engine *app.Engine
	config *sysconfig.SystemConfig
	log    *slog.Logger
	server *http.Server
}

func NewAPIServer(engine *app.Engine, config *sysconfig.SystemConfig, log *slog.Logger) *APIServer {
	return &APIServer{
		engine: engine,
		config: config,
		log:    log.With("component", "api-server"),
	}
}

func Register(lc fx.Lifecycle, s *APIServer) {
	lc.Append(fx.Hook{
		OnStart: s.Start,
		OnStop:  s.Stop,
	})
}

func (s *APIServer) Start(ctx context.Context) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	debug := r.Group("/debug/:exchange")
	debug.Use(s.exchangeValidationMiddleware())
	debug.GET("/funding_rate", s.handleFundingRate)
	debug.GET("/tickers", s.handleTickers)
	debug.GET("/open_positions", s.handleOpenPositions)
	debug.GET("/history_positions", s.handleHistoryPositions)
	debug.GET("/history_orders", s.handleHistoryOrders)
	debug.GET("/order_pnl", s.handleOrderPNL)
	debug.GET("/order/:id", s.handleOrderDetail)
	r.GET("/debug/funding_scanner", s.handleFundingScanner)

	addr := fmt.Sprintf("%s:%d", s.config.APIServer.Host, s.config.APIServer.Port)
	s.server = &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.log.Info("Starting API Server", slog.String("address", addr))
	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("API Server listen and serve failed", slog.Any("error", err))
		}
	}()

	return nil
}

func (s *APIServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	s.log.Info("Stopping API Server")
	return s.server.Shutdown(ctx)
}

func (s *APIServer) exchangeValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		exchangeName := strings.ToLower(c.Param("exchange"))
		prov, err := s.engine.GetProvider(exchangeName)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{errKey: fmt.Sprintf("Exchange %q is not configured or active", exchangeName)})
			c.Abort()
			return
		}
		c.Set("provider", prov)
		c.Next()
	}
}

func (s *APIServer) handleFundingRate(c *gin.Context) {
	s.proxyRoute(c, "funding_rate")
}

func (s *APIServer) handleTickers(c *gin.Context) {
	s.proxyRoute(c, "tickers")
}

func (s *APIServer) handleOpenPositions(c *gin.Context) {
	s.proxyRoute(c, "open_positions")
}

func (s *APIServer) handleHistoryPositions(c *gin.Context) {
	s.proxyRoute(c, "history_positions")
}

func (s *APIServer) handleHistoryOrders(c *gin.Context) {
	s.proxyRoute(c, "history_orders")
}

func (s *APIServer) proxyRoute(c *gin.Context, routeKey string) {
	provVal, ok := c.Get("provider")
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errProviderNotFound})
		return
	}
	prov, ok := provVal.(*app.ExchangeProvider)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errInvalidProviderType})
		return
	}
	rawReq, ok := prov.Client.(exchange.RawRequest)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errRawRequestNotSupport})
		return
	}

	queryParams := parseParams(c)
	exchangeName := strings.ToLower(c.Param("exchange"))
	injectParams(exchangeName, queryParams)

	var respBytes []byte
	var err error

	switch routeKey {
	case "funding_rate":
		respBytes, err = rawReq.GetFundingRateRaw(c, queryParams)
	case "tickers":
		respBytes, err = rawReq.GetTickersRaw(c, queryParams)
	case "open_positions":
		respBytes, err = rawReq.GetOpenPositionsRaw(c, queryParams)
	case "history_positions":
		respBytes, err = rawReq.GetHistoryPositionsRaw(c, queryParams)
	case "history_orders":
		respBytes, err = rawReq.GetHistoryOrdersRaw(c, queryParams)
	default:
		c.JSON(http.StatusBadRequest, gin.H{errKey: "unknown route key"})
		return
	}

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{errKey: err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", respBytes)
}

func (s *APIServer) handleOrderPNL(c *gin.Context) {
	provVal, ok := c.Get("provider")
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errProviderNotFound})
		return
	}
	prov, ok := provVal.(*app.ExchangeProvider)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errInvalidProviderType})
		return
	}

	pnlProv, ok := prov.Client.(exchange.ClosedPnLProvider)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{errKey: errClosedPnLNotSupported})
		return
	}

	symbol := c.Query("symbol")
	orderID := c.Query("order_id")
	if orderID == "" {
		orderID = c.Query("orderId")
	}

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{errKey: "symbol is required"})
		return
	}
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{errKey: "order_id is required"})
		return
	}

	info, err := pnlProv.GetOrderPNL(c.Request.Context(), symbol, orderID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{errKey: err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

func (s *APIServer) handleOrderDetail(c *gin.Context) {
	provVal, ok := c.Get("provider")
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errProviderNotFound})
		return
	}
	prov, ok := provVal.(*app.ExchangeProvider)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errInvalidProviderType})
		return
	}
	rawReq, ok := prov.Client.(exchange.RawRequest)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{errKey: errRawRequestNotSupport})
		return
	}

	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{errKey: errOrderIDRequired})
		return
	}

	queryParams := parseParams(c)
	exchangeName := strings.ToLower(c.Param("exchange"))
	injectParams(exchangeName, queryParams)

	respBytes, err := rawReq.GetOrderDetailRaw(c, orderID, queryParams)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{errKey: err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", respBytes)
}

func parseParams(c *gin.Context) map[string]string {
	params := make(map[string]string)
	for k, vs := range c.Request.URL.Query() {
		if len(vs) > 0 {
			params[k] = vs[0]
		}
	}
	if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut {
		var bodyMap map[string]any
		if err := c.ShouldBindJSON(&bodyMap); err == nil {
			for k, v := range bodyMap {
				params[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	return params
}

func injectParams(exchangeName string, queryParams map[string]string) {
	switch exchangeName {
	case "bybit":
		if queryParams["category"] == "" {
			queryParams["category"] = "linear"
		}
		if queryParams["accountType"] == "" {
			queryParams["accountType"] = "UNIFIED"
		}
	case "okx":
		if queryParams["instType"] == "" {
			queryParams["instType"] = "SWAP"
		}
	}
}
