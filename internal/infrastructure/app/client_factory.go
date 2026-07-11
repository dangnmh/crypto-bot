package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/aevo"
	"crypto-bot/internal/infrastructure/exchange/apex"
	"crypto-bot/internal/infrastructure/exchange/ascendex"
	"crypto-bot/internal/infrastructure/exchange/aster"
	"crypto-bot/internal/infrastructure/exchange/avantis"
	"crypto-bot/internal/infrastructure/exchange/backpack"
	"crypto-bot/internal/infrastructure/exchange/batonex"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitfinex"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitmex"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/blofin"
	"crypto-bot/internal/infrastructure/exchange/btse"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/bydfi"
	"crypto-bot/internal/infrastructure/exchange/coinbase"
	"crypto-bot/internal/infrastructure/exchange/coinex"
	"crypto-bot/internal/infrastructure/exchange/coinw"
	"crypto-bot/internal/infrastructure/exchange/cryptocom"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/delta"
	"crypto-bot/internal/infrastructure/exchange/deribit"
	"crypto-bot/internal/infrastructure/exchange/digifinex"
	"crypto-bot/internal/infrastructure/exchange/dydx"
	"crypto-bot/internal/infrastructure/exchange/extended"
	"crypto-bot/internal/infrastructure/exchange/fameex"
	"crypto-bot/internal/infrastructure/exchange/fmfw"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/gemini"
	"crypto-bot/internal/infrastructure/exchange/grvt"
	"crypto-bot/internal/infrastructure/exchange/hashkey"
	"crypto-bot/internal/infrastructure/exchange/hibt"
	"crypto-bot/internal/infrastructure/exchange/hitbtc"
	"crypto-bot/internal/infrastructure/exchange/hotcoin"
	"crypto-bot/internal/infrastructure/exchange/htx"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/ju"
	"crypto-bot/internal/infrastructure/exchange/jupiter"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/lbank"
	"crypto-bot/internal/infrastructure/exchange/lighter"
	"crypto-bot/internal/infrastructure/exchange/mandala"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	"crypto-bot/internal/infrastructure/exchange/pacifica"
	"crypto-bot/internal/infrastructure/exchange/phemex"
	"crypto-bot/internal/infrastructure/exchange/pionex"
	"crypto-bot/internal/infrastructure/exchange/poloniex"
	"crypto-bot/internal/infrastructure/exchange/sunx"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/tradexyz"
	"crypto-bot/internal/infrastructure/exchange/trubit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/exchange/whitebit"
	"crypto-bot/internal/infrastructure/exchange/woox"
	"crypto-bot/internal/infrastructure/exchange/xt"
	"crypto-bot/internal/infrastructure/exchange/zoomex"
)

// BuildPublicClient creates a standard public REST client for the given exchange.
//
//nolint:cyclop,gocognit,contextcheck // Factory method is naturally complex
func BuildPublicClient(ctx context.Context, exchangeName string, httpClient *http.Client, logger *slog.Logger, logCfg sysconfig.LoggingConfig) (any, error) {
	switch strings.ToLower(exchangeName) {
	case exchange.ExchangeMexc:
		return mexc.NewClient(httpClient, "https://contract.mexc.com", "", "", logCfg), nil
	case exchange.ExchangeAscendex:
		return ascendex.NewClient(httpClient, "https://ascendex.com/api/pro/v2", "", "", logCfg), nil
	case exchange.ExchangeGate:
		return gate.NewClient(httpClient, "https://api.gateio.ws/api/v4", "", "", logCfg), nil
	case exchange.ExchangeBybit:
		return bybit.NewClient(httpClient, "https://api.bybit.com", "", "", "standard", logCfg), nil
	case exchange.ExchangeOkx:
		return okx.NewClient(httpClient, "https://www.okx.com", "", "", "", logCfg), nil
	case exchange.ExchangeKucoin:
		return kucoin.NewClient(httpClient, "https://api-futures.kucoin.com", "", "", "", logCfg), nil
	case exchange.ExchangeBinance:
		return binance.NewClient(httpClient, "https://fapi.binance.com", "", "", logCfg), nil
	case exchange.ExchangeHyperliquid:
		return hyperliquid.NewClient(ctx, httpClient, "https://api.hyperliquid.xyz", "", "", logCfg), nil
	case exchange.ExchangeBitget:
		return bitget.NewClient(httpClient, "https://api.bitget.com", "", "", "", logCfg), nil
	case exchange.ExchangeBingx:
		return bingx.NewClient(httpClient, "https://open-api.bingx.com", "", "", logCfg), nil
	case exchange.ExchangeZoomex:
		return zoomex.NewClient(httpClient, "https://openapi.zoomex.com", logCfg), nil
	case exchange.ExchangeDeepcoin:
		return deepcoin.NewClient(httpClient, "https://api.deepcoin.com", "", "", "", logCfg), nil
	case exchange.ExchangeGemini:
		return gemini.NewClient(httpClient, "https://api.gemini.com", "", "", logCfg), nil
	case exchange.ExchangeToobit:
		return toobit.NewClient(httpClient, "https://api.toobit.com", "", "", logCfg), nil
	case exchange.ExchangeWeex:
		return weex.NewClient(httpClient, "https://api-contract.weex.com", "", "", "", logCfg), nil
	case exchange.ExchangeBatonex:
		return batonex.NewClient(httpClient, "https://api.batonex.com", logCfg), nil
	case exchange.ExchangeBitmart:
		return bitmart.NewClient(httpClient, "https://api-cloud-v2.bitmart.com", "", "", "", logCfg), nil
	case exchange.ExchangeCoinw:
		return coinw.NewClient(httpClient, "https://api.coinw.com", logCfg), nil
	case exchange.ExchangeKrakenFutures:
		return krakenfutures.NewClient(httpClient, "https://futures.kraken.com", logCfg), nil
	case exchange.ExchangeBitunix:
		return bitunix.NewClient(httpClient, "https://fapi.bitunix.com", "", "", logCfg), nil
	case exchange.ExchangeXt:
		return xt.NewClient(httpClient, "https://fapi.xt.com", "", "", logCfg), nil
	case exchange.ExchangeHtx:
		return htx.NewClient(httpClient, "https://api.hbdm.com", logCfg), nil
	case exchange.ExchangeLbank:
		return lbank.NewClient(httpClient, "https://lbkperp.lbank.com", logCfg), nil
	case exchange.ExchangeMandala:
		return mandala.NewClient(httpClient, "https://api.wallet.mandala.exchange/api/3/public", "", "", logCfg), nil
	case exchange.ExchangeOrangex:
		return orangex.NewClient(httpClient, "https://api.orangex.com/api/v1", "", "", logCfg), nil
	case exchange.ExchangePionex:
		return pionex.NewClient(httpClient, "https://api.pionex.com", "", "", logCfg), nil
	case exchange.ExchangePoloniex:
		return poloniex.NewClient(httpClient, "https://api.poloniex.com/v3", "", "", logCfg), nil
	case exchange.ExchangeDeribit:
		return deribit.NewClient(httpClient, "https://www.deribit.com", logCfg), nil
	case exchange.ExchangeDelta:
		return delta.NewClient(httpClient, "https://api.delta.exchange/v2", "", "", logCfg), nil
	case exchange.ExchangeCoinex:
		return coinex.NewClient(httpClient, "https://api.coinex.com/v2", logCfg), nil
	case exchange.ExchangeBitfinex:
		return bitfinex.NewClient(httpClient, "https://api-pub.bitfinex.com", logCfg), nil
	case exchange.ExchangeWhitebit:
		return whitebit.NewClient(httpClient, "https://whitebit.com", logCfg), nil
	case exchange.ExchangeDydx:
		return dydx.NewClient(httpClient, "https://indexer.dydx.trade", logCfg), nil
	case exchange.ExchangeAster:
		return aster.NewClient(httpClient, "https://fapi.asterdex.com", "", "", "", logCfg), nil
	case exchange.ExchangeBackpack:
		return backpack.NewClient(httpClient, "https://api.backpack.exchange/api/v1", "", "", logCfg), nil
	case exchange.ExchangeAevo:
		return aevo.NewClient(httpClient, "https://api.aevo.xyz", "", "", logCfg), nil
	case exchange.ExchangeApex:
		return apex.NewClient(httpClient, "https://omni.apex.exchange", "", "", logCfg), nil
	case exchange.ExchangeLighter:
		return lighter.NewClient(httpClient, "https://mainnet.zklighter.elliot.ai", logCfg), nil
	case exchange.ExchangeTradexyz:
		return tradexyz.NewClient(httpClient, "https://api.hyperliquid.xyz", logCfg), nil
	case exchange.ExchangeGrvt:
		return grvt.NewClient(httpClient, "https://market-data.grvt.io", logCfg), nil
	case exchange.ExchangePacifica:
		return pacifica.NewClient(httpClient, "https://api.pacifica.fi", logCfg), nil
	case exchange.ExchangeExtended:
		return extended.NewClient(httpClient, "https://api.starknet.extended.exchange", logCfg), nil
	case exchange.ExchangeJupiter:
		return jupiter.NewClient(httpClient, "https://perps-api.jup.ag", logCfg), nil
	case exchange.ExchangeAvantis:
		return avantis.NewClient(httpClient, "https://data.avantisfi.com", logCfg), nil
	case exchange.ExchangeBtse:
		return btse.NewClient(httpClient, "https://api.btse.com/futures/api/v2.1", "", "", logCfg), nil
	case exchange.ExchangeBitmex:
		return bitmex.NewClient(httpClient, "https://www.bitmex.com", logCfg), nil
	case exchange.ExchangeHashkey:
		return hashkey.NewClient(httpClient, "https://api-glb.hashkey.com", logger), nil
	case exchange.ExchangeHibt:
		return hibt.NewClient(httpClient, "https://fapi.hibt0.com/open-api", logCfg), nil
	case exchange.ExchangeHitbtc:
		return hitbtc.NewClient(httpClient, "https://api.hitbtc.com/api/3/public", "", "", logCfg), nil
	case exchange.ExchangeHotcoin:
		return hotcoin.NewClient(httpClient, "https://api-ct.hotcoin.fit", "", "", logCfg), nil
	case exchange.ExchangeCryptocom:
		return cryptocom.NewClient(httpClient, "https://deriv-api.crypto.com/v1", logger), nil
	case exchange.ExchangeWoox:
		return woox.NewClient(httpClient, "https://api.woox.io", logger), nil
	case exchange.ExchangePhemex:
		return phemex.NewClient(httpClient, "https://api.phemex.com", logger), nil
	case exchange.ExchangeBlofin:
		return blofin.NewClient(httpClient, "https://openapi.blofin.com", logger), nil
	case exchange.ExchangeDigifinex:
		return digifinex.NewClient(httpClient, "https://openapi.digifinex.com", logger), nil
	case exchange.ExchangeBydfi:
		return bydfi.NewClient(httpClient, "https://api.bydfi.com/api", logger), nil
	case exchange.ExchangeJu:
		return ju.NewClient(httpClient, "https://api.jucoin.com", logCfg), nil
	case exchange.ExchangeCoinbase:
		return coinbase.NewClient(httpClient, "https://api.international.coinbase.com", logCfg), nil
	case "sunx":
		return sunx.NewClient(httpClient, "https://api.sunx.com", logCfg), nil
	case exchange.ExchangeTrubit:
		return trubit.NewClient(httpClient, "https://api.trubit.com", logCfg), nil
	case exchange.ExchangeFameex:
		return fameex.NewClient(httpClient, "https://api.fameex.com", logCfg), nil
	case "fmfw":
		return fmfw.NewClient(httpClient, "https://api.fmfw.io/api/3/public", "", "", logCfg), nil
	default:
		return nil, fmt.Errorf("unsupported exchange %q", exchangeName)
	}
}
