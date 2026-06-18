package main

import (
	"flag"
	"time"

	"crypto-bot/internal/bots/funding/bootstrap"

	"go.uber.org/fx"
)

func main() {
	sysCfgPath := flag.String("sys", "./configs/funding/local/system.jsonc", "path to system config")
	exchCfgPath := flag.String("exch", "./configs/funding/local/exchange.jsonc", "path to exchange config")
	botCfgPath := flag.String("bot", "./configs/funding/local/funding.jsonc", "path to bot config")
	flag.Parse()

	fx.New(
		bootstrap.Module(bootstrap.ConfigPaths{
			System:   *sysCfgPath,
			Exchange: *exchCfgPath,
			Bot:      *botCfgPath,
		}),
		fx.StartTimeout(2*time.Minute),
		fx.StopTimeout(10*time.Second),
	).Run()
}
