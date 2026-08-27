//  Copyright 2026 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

//go:build benchmark

package packages

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/osconfig/osinfo"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltrace"
)

type cpuSample struct {
	totalTicks  uint64
	ticksPerSec float64
	time        time.Time
}

type traceMetricsResult struct {
	utiltrace.TraceMemoryResult
	CPUPeakPercent float64
	CPUMeanPercent float64
}

func traceMetrics(ctx context.Context, interval time.Duration, resultChan chan<- traceMetricsResult) {
	memChan := make(chan utiltrace.TraceMemoryResult, 1)
	ctxMemory, cancelMem := context.WithCancel(ctx)
	go utiltrace.TraceMemory(ctxMemory, interval, memChan)

	var lastSample cpuSample
	if ticks, tps, err := readCPUTicks(); err == nil {
		lastSample = cpuSample{totalTicks: ticks, ticksPerSec: tps, time: time.Now()}
	}

	var peakCPU, runningAverageCPU float64
	sampleCount := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentTicks, tps, err := readCPUTicks()
			now := time.Now()
			if err == nil && !lastSample.time.IsZero() {
				timeDelta := now.Sub(lastSample.time).Seconds()
				ticksDelta := float64(currentTicks - lastSample.totalTicks)
				if timeDelta > 0 && lastSample.ticksPerSec > 0 {
					cpuPercent := (ticksDelta / lastSample.ticksPerSec) / timeDelta * 100.0
					sampleCount++
					runningAverageCPU += (cpuPercent - runningAverageCPU) / float64(sampleCount)
					if cpuPercent > peakCPU {
						peakCPU = cpuPercent
					}
				}
				lastSample = cpuSample{totalTicks: currentTicks, ticksPerSec: tps, time: now}
			}
		case <-ctx.Done():
			cancelMem()
			memResult := <-memChan
			resultChan <- traceMetricsResult{
				TraceMemoryResult: memResult,
				CPUPeakPercent:    peakCPU,
				CPUMeanPercent:    runningAverageCPU,
			}
			return
		}
	}
}

type benchResult struct {
	duration  time.Duration
	allocMB   float64
	memPeakMB float64
	cpuPeak   float64
	cpuMean   float64
	pkgsCount int
}

// runSingleBenchmark runs a single iteration of installed packages extraction with metrics tracing.
func runSingleBenchmark(ctx context.Context, osinfoProvider osinfo.Provider, extractors []string) (benchResult, error) {
	runtime.GC()

	traceCtx, cancelTrace := context.WithCancel(ctx)
	resChan := make(chan traceMetricsResult, 1)
	go traceMetrics(traceCtx, 20*time.Millisecond, resChan)

	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	start := time.Now()
	provider := &scalibrInstalledPackagesProvider{
		extractors:     extractors,
		osinfoProvider: osinfoProvider,
	}
	pkgs, err := provider.GetInstalledPackages(ctx)
	elapsed := time.Since(start)

	runtime.ReadMemStats(&memAfter)
	cancelTrace()

	metrics := <-resChan
	if err != nil {
		return benchResult{}, fmt.Errorf("GetInstalledPackages error: %w", err)
	}

	pkgsCount := len(pkgs.Chocolatey) + len(pkgs.WinGet) + len(pkgs.Deb) + len(pkgs.Rpm) + len(pkgs.COS)
	allocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024

	return benchResult{
		duration:  elapsed,
		allocMB:   allocMB,
		memPeakMB: metrics.MemPeakMB,
		cpuPeak:   metrics.CPUPeakPercent,
		cpuMean:   metrics.CPUMeanPercent,
		pkgsCount: pkgsCount,
	}, nil
}

// calcP95 computes the 95th percentile duration from a slice of durations.
func calcP95(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// logBenchResult calculates averages over runs and outputs the formatted benchmark table row.
func logBenchResult(t *testing.T, name string, res benchResult, p95Duration time.Duration, runs int) {
	if runs <= 0 {
		return
	}
	r := float64(runs)
	avgDuration := res.duration / time.Duration(runs)
	avgAllocMB := res.allocMB / r
	avgPeakRAM := res.memPeakMB / r
	avgPeakCPU := res.cpuPeak / r
	avgMeanCPU := res.cpuMean / r

	t.Logf("| `%s` | %v | %v | %.2f MB | %.2f MB | %.1f%% | %.1f%% | %d |",
		name,
		avgDuration,
		p95Duration,
		avgAllocMB,
		avgPeakRAM,
		avgPeakCPU,
		avgMeanCPU,
		res.pkgsCount,
	)
}

func TestScalibrBenchmark(t *testing.T) {
	benchmarks := []struct {
		name       string
		extractors []string
	}{
		{name: "All os extractors", extractors: []string{"os/chocolatey", "os/winget"}},
		{name: "os/chocolatey", extractors: []string{"os/chocolatey"}},
		{name: "os/winget", extractors: []string{"os/winget"}},
	}

	ctx := context.Background()
	osinfoProvider := osinfo.NewProvider()

	t.Log("\n=========================================================================")
	t.Log("### SCALIBR Extractors Benchmark Results")
	t.Log("=========================================================================")
	t.Log("| Scenario | Avg Scan Time | p95 Scan Time | Avg Heap Alloc | Peak RAM RSS | Peak CPU % | Mean CPU % | Pkgs Found |")
	t.Log("| --- | --- | --- | --- | --- | --- | --- | --- |")

	runs := 3
	for _, bm := range benchmarks {
		var total benchResult
		var durations []time.Duration
		for i := 0; i < runs; i++ {
			res, err := runSingleBenchmark(ctx, osinfoProvider, bm.extractors)
			if err != nil {
				t.Fatalf("Benchmark scenario %s failed: %v", bm.name, err)
			}
			durations = append(durations, res.duration)
			total.duration += res.duration
			total.allocMB += res.allocMB
			total.memPeakMB += res.memPeakMB
			total.cpuPeak += res.cpuPeak
			total.cpuMean += res.cpuMean
			total.pkgsCount = res.pkgsCount
		}

		p95 := calcP95(durations)
		logBenchResult(t, bm.name, total, p95, runs)
	}
}

func TestCompareWinGetScalibrAndLegacy(t *testing.T) {
	ctx := context.Background()
	osinfoProvider := osinfo.NewProvider()

	t.Log("Collecting WinGet packages via SCALIBR...")
	scalibrProvider := &scalibrInstalledPackagesProvider{
		extractors:     []string{"os/winget"},
		osinfoProvider: osinfoProvider,
	}
	scalibrPkgs, err := scalibrProvider.GetInstalledPackages(ctx)
	if err != nil {
		t.Fatalf("SCALIBR GetInstalledPackages failed: %v", err)
	}

	t.Log("Collecting Windows applications via Legacy flow (Registry)...")
	legacyProvider := &defaultInstalledPackagesProvider{
		osinfoProvider: osinfoProvider,
	}
	legacyPkgs, err := legacyProvider.GetInstalledPackages(ctx)
	if err != nil {
		t.Logf("Legacy GetInstalledPackages returned warnings/errors: %v", err)
	}

	t.Log("\n=========================================================================")
	t.Log("### WinGet (SCALIBR) vs Legacy Windows Applications Comparison")
	t.Log("=========================================================================")
	t.Logf("| Flow | Total Packages Found |")
	t.Log("| --- | --- |")
	t.Logf("| SCALIBR (`os/winget`) | %d |", len(scalibrPkgs.WinGet))
	t.Logf("| Legacy (`WindowsApplication`) | %d |", len(legacyPkgs.WindowsApplication))

	t.Log("\n--- SCALIBR WinGet Packages ---")
	for i, pkg := range scalibrPkgs.WinGet {
		t.Logf("  [%d] Name: %s | Version: %s | PURL: %s", i+1, pkg.Name, pkg.Version, pkg.Purl)
	}

	t.Log("\n--- Legacy Windows Applications (First 20) ---")
	for i, app := range legacyPkgs.WindowsApplication {
		if i >= 20 {
			t.Logf("  ... and %d more legacy applications", len(legacyPkgs.WindowsApplication)-20)
			break
		}
		t.Logf("  [%d] DisplayName: %s | DisplayVersion: %s | Publisher: %s", i+1, app.DisplayName, app.DisplayVersion, app.Publisher)
	}

	t.Log("\n--- Cross-Match Analysis ---")
	matchedCount := 0
	for _, wingetPkg := range scalibrPkgs.WinGet {
		foundInLegacy := false
		for _, app := range legacyPkgs.WindowsApplication {
			if strings.EqualFold(wingetPkg.Name, app.DisplayName) ||
				strings.Contains(strings.ToLower(app.DisplayName), strings.ToLower(wingetPkg.Name)) ||
				strings.Contains(strings.ToLower(wingetPkg.Name), strings.ToLower(app.DisplayName)) {
				t.Logf("  [MATCH] SCALIBR: '%s' (%s) <===> Legacy: '%s' (%s)", wingetPkg.Name, wingetPkg.Version, app.DisplayName, app.DisplayVersion)
				foundInLegacy = true
				matchedCount++
				break
			}
		}
		if !foundInLegacy {
			t.Logf("  [SCALIBR ONLY] '%s' (%s) - no matching DisplayName in Registry", wingetPkg.Name, wingetPkg.Version)
		}
	}
	t.Logf("\nSummary: %d of %d SCALIBR WinGet packages matched in Legacy Registry inventory.", matchedCount, len(scalibrPkgs.WinGet))
}