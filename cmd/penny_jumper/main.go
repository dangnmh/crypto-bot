package main

import (
	"flag"
	"time"

	"crypto-bot/internal/bots/penny_jumper/bootstrap"

	"go.uber.org/fx"
)

func main() {
	sysCfgPath := flag.String("sys", "./configs/penny_jumper/local/system.jsonc", "path to system config")
	exchCfgPath := flag.String("exch", "./configs/penny_jumper/local/exchange.jsonc", "path to exchange config")
	botCfgPath := flag.String("bot", "./configs/penny_jumper/local/penny_jumper.jsonc", "path to penny jumper bot config")
	blacklistCfgPath := flag.String("blacklist", "./configs/penny_jumper/local/blacklist.jsonc", "path to blacklist config")
	flag.Parse()

	fx.New(
		bootstrap.Module(bootstrap.ConfigPaths{
			System:    *sysCfgPath,
			Exchange:  *exchCfgPath,
			Bot:       *botCfgPath,
			Blacklist: *blacklistCfgPath,
		}),
		fx.StartTimeout(2*time.Minute),
		fx.StopTimeout(10*time.Second),
	).Run()
}
