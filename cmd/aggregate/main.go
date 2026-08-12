package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type RunResult struct {
	Strategy            string  `json:"strategy"`
	Mode                string  `json:"mode"`
	LoadLevelPct        int     `json:"load_level_pct"`
	Seed                int64   `json:"seed"`
	TotalJobs           int     `json:"total_jobs"`
	TotalDemandUnits    int     `json:"total_demand_units"`
	Scheduled           int     `json:"scheduled"`
	Pending             int     `json:"pending"`
	DeniedByWebhook     int     `json:"denied_by_webhook"`
	TotalScheduledUnits int     `json:"total_scheduled_units"`
	MaxFreeCompute      int     `json:"max_free_compute"`
	TotalFreeCompute    int     `json:"total_free_compute"`
	UtilizationPct      float64 `json:"utilization_pct"`
	IdleDollarsWasted   float64 `json:"idle_dollars_wasted"`
	CostPerJob          float64 `json:"cost_per_job"`
	ProbeJobSucceeded   bool    `json:"probe_job_succeeded"`
	Valid               bool    `json:"valid"`
}

type key struct {
	strategy string
	loadPct  int
}

type aggregate struct {
	count             int
	invalidCount      int
	sumScheduledPct   float64 // scheduled units / demand units, per run
	sumMaxFree        float64
	sumTotalFree      float64
	sumUtilization    float64
	sumIdleWaste      float64
	sumCostPerJob     float64
	probeSuccessCount int
	sumDeniedOrPending float64 // (pending + denied) / total jobs, per run
}

func main() {
	resultsDir := "results"
	if len(os.Args) > 1 {
		resultsDir = os.Args[1]
	}
	outputPath := "RESULTS.md"
	if len(os.Args) > 2 {
		outputPath = os.Args[2]
	}

	files, err := filepath.Glob(filepath.Join(resultsDir, "*.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list result files: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no result files found in %s\n", resultsDir)
		os.Exit(1)
	}

	aggregates := make(map[key]*aggregate)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", f, err)
			continue
		}
		var r RunResult
		if err := json.Unmarshal(data, &r); err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s (parse error): %v\n", f, err)
			continue
		}

		k := key{strategy: r.Strategy, loadPct: r.LoadLevelPct}
		a, ok := aggregates[k]
		if !ok {
			a = &aggregate{}
			aggregates[k] = a
		}

		if !r.Valid {
			a.invalidCount++
			continue
		}

		a.count++
		if r.TotalDemandUnits > 0 {
			a.sumScheduledPct += 100.0 * float64(r.TotalScheduledUnits) / float64(r.TotalDemandUnits)
		}
		a.sumMaxFree += float64(r.MaxFreeCompute)
		a.sumTotalFree += float64(r.TotalFreeCompute)
		a.sumUtilization += r.UtilizationPct
		a.sumIdleWaste += r.IdleDollarsWasted
		a.sumCostPerJob += r.CostPerJob
		if r.ProbeJobSucceeded {
			a.probeSuccessCount++
		}
		if r.TotalJobs > 0 {
			a.sumDeniedOrPending += 100.0 * float64(r.Pending+r.DeniedByWebhook) / float64(r.TotalJobs)
		}
	}

	var keys []key
	for k := range aggregates {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].loadPct != keys[j].loadPct {
			return keys[i].loadPct < keys[j].loadPct
		}
		return keys[i].strategy < keys[j].strategy
	})

	var md string
	md += "# Benchmark Results\n\n"
	md += fmt.Sprintf("Aggregated from %d individual runs.\n\n", len(files))
	md += "| Load | Strategy | Runs | Avg Scheduled Units % | Avg Denied/Pending % | Avg Max Free/GPU | Avg Utilization % | Avg Idle $ Wasted | Avg Cost/Job | Probe Success Rate |\n"
	md += "|---|---|---|---|---|---|---|---|---|---|\n"

	fmt.Println("=== Aggregated Results ===\n")
	fmt.Printf("%-6s %-16s %-6s %-10s %-10s %-10s %-8s %-10s %-10s %-8s\n",
		"Load", "Strategy", "Runs", "Sched%", "Den/Pend%", "MaxFree", "Util%", "IdleWaste", "Cost/Job", "Probe%")

	for _, k := range keys {
		a := aggregates[k]
		if a.count == 0 {
			continue
		}
		n := float64(a.count)
		schedPct := a.sumScheduledPct / n
		deniedPct := a.sumDeniedOrPending / n
		maxFree := a.sumMaxFree / n
		totalFree := a.sumTotalFree / n
		util := a.sumUtilization / n
		idleWaste := a.sumIdleWaste / n
		costPerJob := a.sumCostPerJob / n
		probeRate := 100.0 * float64(a.probeSuccessCount) / n

		fmt.Printf("%-6d %-16s %-6d %-10.1f %-10.1f %-10.1f %-8.1f $%-9.2f $%-9.3f %-8.1f\n",
			k.loadPct, k.strategy, a.count, schedPct, deniedPct, maxFree, util, idleWaste, costPerJob, probeRate)

		md += fmt.Sprintf("| %d%% | %s | %d%s | %.1f%% | %.1f%% | %.1f/10 (total free %.1f/60) | %.1f%% | $%.2f | $%.3f | %.1f%% |\n",
			k.loadPct, k.strategy, a.count,
			ifInvalid(a.invalidCount),
			schedPct, deniedPct, maxFree, totalFree, util, idleWaste, costPerJob, probeRate)
	}

	if err := os.WriteFile(outputPath, []byte(md), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}
	fmt.Println("\nWrote " + outputPath)
}

func ifInvalid(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d invalid, excluded)", n)
}
