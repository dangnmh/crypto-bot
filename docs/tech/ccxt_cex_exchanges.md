# CCXT Centralized Exchanges (CEX) sorted by Popularity

This file lists the CEX exchanges supported by CCXT, sorted by popularity (CoinGecko Trust Score Rank / Certified status), and indicates whether they are currently supported by our scanner (`tools/scanner/main.go`).

| Rank | Exchange ID | Exchange Name | CCXT Certified? | CoinGecko Trust Score | Scanner Supported? | Notes |
|------|-------------|---------------|-----------------|-----------------------|--------------------|-------|
| 1 | `coinbaseexchange` | Coinbase Exchange | No | 10/10 | ❌ No | CoinGecko Rank: #1 |
| 2 | `binance` | Binance | ✅ Yes | 10/10 | ✅ Supported (`binance`) | CoinGecko Rank: #2 |
| 3 | `binancecoinm` | Binance COIN-M | ✅ Yes | 10/10 | ✅ Supported (`binance`) | CoinGecko Rank: #2 |
| 4 | `binanceusdm` | Binance USDⓈ-M | ✅ Yes | 10/10 | ✅ Supported (`binance`) | CoinGecko Rank: #2 |
| 5 | `kraken` | Kraken | No | 10/10 | ✅ Yes | CoinGecko Rank: #3 |
| 6 | `krakenfutures` | Kraken Futures | No | 10/10 | ✅ Supported (`krakenfutures`) | CoinGecko Rank: #3 |
| 7 | `bitget` | Bitget | ✅ Yes | 10/10 | ✅ Supported (`bitget`) | CoinGecko Rank: #4 |
| 8 | `myokx` | MyOKX (EEA) | No | 10/10 | ❌ No | CoinGecko Rank: #5 |
| 9 | `okx` | OKX | ✅ Yes | 10/10 | ✅ Supported (`okx`) | CoinGecko Rank: #5 |
| 10 | `okxus` | OKX (US) | No | 10/10 | ✅ Supported (`okx`) | CoinGecko Rank: #5 |
| 11 | `bybit` | Bybit | ✅ Yes | 9/10 | ✅ Supported (`bybit`) | CoinGecko Rank: #6 |
| 12 | `bitstamp` | Bitstamp | No | 9/10 | ❌ No | CoinGecko Rank: #7 |
| 13 | `gate` | Gate | ✅ Yes | 9/10 | ✅ Supported (`gate`) | CoinGecko Rank: #8 |
| 14 | `mexc` | MEXC Global | ✅ Yes | 9/10 | ✅ Supported (`mexc`) | CoinGecko Rank: #9 |
| 15 | `bitvavo` | Bitvavo | No | 9/10 | ❌ No | CoinGecko Rank: #11 |
| 16 | `kucoin` | KuCoin | ✅ Yes | 9/10 | ✅ Supported (`kucoin`) | CoinGecko Rank: #12 |
| 17 | `bingx` | BingX | ✅ Yes | 9/10 | ✅ Supported (`bingx`) | CoinGecko Rank: #13 |
| 18 | `bitso` | Bitso | No | 9/10 | ❌ No | CoinGecko Rank: #14 |
| 19 | `cryptocom` | Crypto.com | ✅ Yes | 9/10 | ✅ Supported (`cryptocom`) | CoinGecko Rank: #15 |
| 20 | `lbank` | LBank | No | 8/10 | ✅ Supported (`lbank`) | CoinGecko Rank: #16 |
| 21 | `hashkey` | HashKey Global | ✅ Yes | 8/10 | ✅ Supported (`hashkey`) | CoinGecko Rank: #17 |
| 22 | `gemini` | Gemini | No | 8/10 | ❌ No | CoinGecko Rank: #18 |
| 23 | `bullish` | Bullish | No | 8/10 | ❌ No | CoinGecko Rank: #20 |
| 24 | `binanceus` | Binance US | No | 8/10 | ✅ Supported (`binance`) | CoinGecko Rank: #21 |
| 25 | `luno` | luno | No | 8/10 | ❌ No | CoinGecko Rank: #23 |
| 26 | `upbit` | Upbit | No | 8/10 | ❌ No | CoinGecko Rank: #24 |
| 27 | `whitebit` | WhiteBit | No | 8/10 | ✅ Supported (`whitebit`) | CoinGecko Rank: #26 |
| 28 | `weex` | Weex | No | 8/10 | ✅ Supported (`weex`) | CoinGecko Rank: #28 |
| 29 | `toobit` | Toobit | No | 8/10 | ✅ Supported (`toobit`) | CoinGecko Rank: #29 |
| 30 | `backpack` | Backpack | No | 8/10 | ❌ No | CoinGecko Rank: #31 |
| 31 | `bitbank` | bitbank | No | 8/10 | ❌ No | CoinGecko Rank: #32 |
| 32 | `bitmart` | BitMart | ✅ Yes | 8/10 | ✅ Supported (`bitmart`) | CoinGecko Rank: #33 |
| 33 | `digifinex` | DigiFinex | ✅ Yes | 8/10 | ✅ Supported (`digifinex`) | CoinGecko Rank: #36 |
| 34 | `bybiteu` | Bybit EU | No | 8/10 | ✅ Supported (`bybit`) | CoinGecko Rank: #37 |
| 35 | `bitmex` | BitMEX | ✅ Yes | 8/10 | ✅ Supported (`bitmex`) | CoinGecko Rank: #41 |
| 36 | `coinsph` | Coins.ph | No | 7/10 | ❌ No | CoinGecko Rank: #43 |
| 37 | `deribit` | Deribit | No | 7/10 | ✅ Supported (`deribit`) | CoinGecko Rank: #46 |
| 38 | `indodax` | INDODAX | No | 7/10 | ❌ No | CoinGecko Rank: #47 |
| 39 | `bitrue` | Bitrue | No | 7/10 | ❌ No | CoinGecko Rank: #48 |
| 40 | `bitfinex` | Bitfinex | No | 7/10 | ✅ Supported (`bitfinex`) | CoinGecko Rank: #52 |
| 41 | `bithumb` | Bithumb | No | 7/10 | ❌ No | CoinGecko Rank: #53 |
| 42 | `htx` | HTX | ✅ Yes | 7/10 | ✅ Supported (`htx`) | CoinGecko Rank: #57 |
| 43 | `phemex` | Phemex | No | 7/10 | ✅ Supported (`phemex`) | CoinGecko Rank: #59 |
| 44 | `bydfi` | BYDFi | ✅ Yes | 7/10 | ✅ Supported (`bydfi`) | CoinGecko Rank: #60 |
| 45 | `p2b` | p2b | No | 7/10 | ❌ No | CoinGecko Rank: #61 |
| 46 | `xt` | XT | No | 7/10 | ✅ Supported (`xt`) | CoinGecko Rank: #63 |
| 47 | `blofin` | BloFin | ✅ Yes | 7/10 | ✅ Supported (`blofin`) | CoinGecko Rank: #64 |
| 48 | `cex` | CEX.IO | No | 7/10 | ❌ No | CoinGecko Rank: #68 |
| 49 | `bitflyer` | bitFlyer | No | 7/10 | ❌ No | CoinGecko Rank: #75 |
| 50 | `bittrade` | BitTrade | No | 7/10 | ❌ No | CoinGecko Rank: #76 |
| 51 | `coinone` | CoinOne | No | 7/10 | ❌ No | CoinGecko Rank: #77 |
| 52 | `ascendex` | AscendEX | No | 6/10 | ❌ No | CoinGecko Rank: #79 |
| 53 | `coinmetro` | Coinmetro | No | 6/10 | ❌ No | CoinGecko Rank: #80 |
| 54 | `independentreserve` | Independent Reserve | No | 6/10 | ❌ No | CoinGecko Rank: #81 |
| 55 | `btcturk` | BTCTurk | No | 6/10 | ❌ No | CoinGecko Rank: #86 |
| 56 | `bitopro` | BitoPro | No | 6/10 | ❌ No | CoinGecko Rank: #90 |
| 57 | `coinex` | CoinEx | ✅ Yes | 6/10 | ✅ Supported (`coinex`) | CoinGecko Rank: #93 |
| 58 | `tokocrypto` | Tokocrypto | No | 6/10 | ❌ No | CoinGecko Rank: #96 |
| 59 | `coincheck` | coincheck | No | 5/10 | ❌ No | CoinGecko Rank: #100 |
| 60 | `mercado` | Mercado Bitcoin | No | 5/10 | ❌ No | CoinGecko Rank: #101 |
| 61 | `bigone` | BigONE | No | 5/10 | ❌ No | CoinGecko Rank: #104 |
| 62 | `zaif` | Zaif | No | 5/10 | ❌ No | CoinGecko Rank: #105 |
| 63 | `blockchaincom` | Blockchain.com | No | 5/10 | ❌ No | CoinGecko Rank: #107 |
| 64 | `deepcoin` | DeepCoin | No | 5/10 | ✅ Supported (`deepcoin`) | CoinGecko Rank: #110 |
| 65 | `poloniex` | Poloniex | No | 5/10 | ❌ No | CoinGecko Rank: #114 |
| 66 | `fmfwio` | FMFW.io | No | 5/10 | ❌ No | CoinGecko Rank: #116 |
| 67 | `woo` | WOO X | ✅ Yes | 4/10 | ✅ Supported (`woo`) | CoinGecko Rank: #118 |
| 68 | `btcmarkets` | BTC Markets | No | 4/10 | ❌ No | CoinGecko Rank: #120 |
| 69 | `novadax` | NovaDAX | No | 4/10 | ❌ No | CoinGecko Rank: #123 |
| 70 | `exmo` | EXMO | No | 4/10 | ❌ No | CoinGecko Rank: #127 |
| 71 | `foxbit` | Foxbit | No | 4/10 | ❌ No | CoinGecko Rank: #129 |
| 72 | `onetrading` | One Trading | No | 4/10 | ❌ No | CoinGecko Rank: #132 |
| 73 | `paymium` | Paymium | No | 3/10 | ❌ No | CoinGecko Rank: #143 |
| 74 | `latoken` | Latoken | No | 3/10 | ❌ No | CoinGecko Rank: #145 |
| 75 | `hitbtc` | HitBTC | No | 3/10 | ❌ No | CoinGecko Rank: #157 |
| 76 | `bitbns` | Bitbns | No | 3/10 | ❌ No | CoinGecko Rank: #160 |
| 77 | `zebpay` | Zebpay | No | 3/10 | ❌ No | CoinGecko Rank: #163 |
| 78 | `bit2c` | Bit2C | No | 3/10 | ❌ No | CoinGecko Rank: #164 |
| 79 | `ndax` | NDAX | No | 2/10 | ❌ No | CoinGecko Rank: #169 |
| 80 | `coinbaseinternational` | Coinbase International | No | 2/10 | ❌ No | CoinGecko Rank: #176 |
| 81 | `kucoinfutures` | KuCoin Futures | ✅ Yes | N/A | ✅ Supported (`kucoin`) | No CoinGecko Rank (Prioritized via CCXT Certification) |
| 82 | `alpaca` | Alpaca | No | N/A | ❌ No | No CoinGecko Rank |
| 83 | `bequant` | Bequant | No | N/A | ❌ No | No CoinGecko Rank |
| 84 | `bitteam` | BIT.TEAM | No | N/A | ❌ No | No CoinGecko Rank |
| 85 | `btcbox` | BtcBox | No | N/A | ❌ No | No CoinGecko Rank |
| 86 | `coinbase` | Coinbase Advanced | No | N/A | ❌ No | No CoinGecko Rank |
| 87 | `coinmate` | CoinMate | No | N/A | ❌ No | No CoinGecko Rank |
| 88 | `coinspot` | CoinSpot | No | N/A | ❌ No | No CoinGecko Rank |
| 89 | `cryptomus` | Cryptomus | No | N/A | ❌ No | No CoinGecko Rank |
| 90 | `delta` | Delta Exchange | No | N/A | ❌ No | No CoinGecko Rank |
| 91 | `hollaex` | HollaEx | No | N/A | ❌ No | No CoinGecko Rank |
