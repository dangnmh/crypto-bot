package main

import (
	"flag"
	"time"

	"crypto-bot/internal/bots/funding/bootstrap"

	"go.uber.org/fx"
)

func main() {
	accountsCfgPath := flag.String("accounts", "./configs/funding/local/accounts.jsonc", "path to accounts manifest config")
	sysCfgPath := flag.String("sys", "./configs/funding/local/system.jsonc", "path to system config")
	exchCfgPath := flag.String("exch", "./configs/funding/local/exchange.jsonc", "path to exchange config")
	blacklistCfgPath := flag.String("blacklist", "./configs/funding/local/blacklist.jsonc", "path to blacklist config")
	reversionCfgPath := flag.String("reversion", "./configs/funding/local/reversion.jsonc", "path to common reversion config")
	flag.Parse()

	fx.New(
		bootstrap.Module(bootstrap.ConfigPaths{
			Accounts:  *accountsCfgPath,
			System:    *sysCfgPath,
			Exchange:  *exchCfgPath,
			Blacklist: *blacklistCfgPath,
			Reversion: *reversionCfgPath,
		}),
		fx.StartTimeout(2*time.Minute),
		fx.StopTimeout(10*time.Second),
	).Run()
}
