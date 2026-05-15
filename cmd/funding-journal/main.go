package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"crypto-bot/internal/bots/funding/journalreport"
)

func main() {
	dir := flag.String("dir", "data/journal", "journal directory")
	dateStr := flag.String("date", time.Now().Format("2006-01-02"), "journal date YYYY-MM-DD")
	symbol := flag.String("symbol", "", "optional symbol filter")
	jsonOut := flag.Bool("json", false, "print JSON report")
	flag.Parse()

	date, err := time.Parse("2006-01-02", *dateStr)
	if err != nil {
		log.Fatalf("invalid -date: %v", err)
	}

	records, err := journalreport.Load(journalreport.Query{
		Dir:    *dir,
		Date:   date,
		Symbol: *symbol,
	})
	if err != nil {
		log.Fatal(err)
	}

	report := journalreport.Build(date, *symbol, records)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatal(err)
		}
		return
	}

	printText(report)
}

func printText(r journalreport.Report) {
	scope := r.Date
	if r.Symbol != "" {
		scope += " " + r.Symbol
	}

	fmt.Printf("Funding Journal Report %s\n", scope)
	fmt.Printf("Cycles: %d\n", r.Cycles)
	fmt.Printf("Outcomes: %v\n", r.Outcomes)
	fmt.Printf("IOC: fill %.1f%%, avg slippage %.4f%%, settle median %.0fms (avg %.0fms, min %dms, max %dms)\n",
		r.IOC.FillRatePct,
		r.IOC.AvgSlippagePct,
		r.IOC.SettleOffsetMedianMs,
		r.IOC.SettleOffsetAvgMs,
		r.IOC.SettleOffsetMinMs,
		r.IOC.SettleOffsetMaxMs,
	)
	fmt.Printf("IOC excursion: avg MFE %.4f%%, avg MAE %.4f%%\n", r.IOC.AvgMFEPct, r.IOC.AvgMAEPct)
	fmt.Printf("Trap: enabled %d, fill %.1f%%, avg MFE %.4f%%, avg MAE %.4f%%, by source %v\n",
		r.Trap.EnabledCycles,
		r.Trap.FillRatePct,
		r.Trap.AvgMFEPct,
		r.Trap.AvgMAEPct,
		r.Trap.BySource,
	)
	if len(r.UnitWarnings) > 0 {
		fmt.Println("Unit warnings:")
		for _, warning := range r.UnitWarnings {
			fmt.Printf("- %s\n", warning)
		}
	}
	if len(r.Recommendations) > 0 {
		fmt.Println("Recommendations:")
		for _, recommendation := range r.Recommendations {
			fmt.Printf("- %s\n", recommendation)
		}
	}
}
