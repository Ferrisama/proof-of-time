package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"sync"
	"time"
)

// StressTestConfig holds configuration for stress testing
type StressTestConfig struct {
	// Number of concurrent VDF computations
	ConcurrentWorkers int
	// Total number of test runs
	TotalRuns int
	// Target delay for each VDF computation (seconds)
	TargetDelaySeconds float64
	// Modulus size in bits (0 = use 2048-bit RSA)
	ModulusBits int
	// Whether to generate Wesolowski proofs
	GenerateWesolowski bool
	// Whether to verify proofs
	VerifyProofs bool
}

// StressTestResult contains results from a single test
type StressTestResult struct {
	// Computation time in seconds
	ComputationTime float64
	// Steps executed
	Steps int64
	// Proof generation time (if applicable)
	ProofGenTime float64
	// Proof verification time (if applicable)
	ProofVerifyTime float64
	// Success flag
	Success bool
	// Error message if failed
	Error string
}

// StressTestSummary contains aggregate test results
type StressTestSummary struct {
	// Configuration used
	Config StressTestConfig
	// Individual test results
	Results []*StressTestResult
	// Total duration
	TotalDuration time.Duration
	// Timing statistics
	ComputationStats *TimingStats
	ProofGenStats    *TimingStats
	ProofVerifyStats *TimingStats
	// Throughput: proofs per second
	ProofsPerSecond float64
	// Success rate
	SuccessRate float64
}

// TimingStats holds statistics about timing
type TimingStats struct {
	Min    float64
	Max    float64
	Mean   float64
	Median float64
	StdDev float64
	P95    float64
	P99    float64
}

// RunStressTest executes a comprehensive stress test
func RunStressTest(config StressTestConfig) *StressTestSummary {
	fmt.Printf("\n🚀 Starting Stress Test\n")
	fmt.Printf("========================\n")
	fmt.Printf("Workers: %d, Total Runs: %d, Target Delay: %.1f seconds\n",
		config.ConcurrentWorkers, config.TotalRuns, config.TargetDelaySeconds)
	fmt.Printf("Generate Wesolowski: %v, Verify: %v\n",
		config.GenerateWesolowski, config.VerifyProofs)

	startTime := time.Now()

	// Create work channel and result channel
	workChan := make(chan int, config.TotalRuns)
	resultChan := make(chan *StressTestResult, config.TotalRuns)

	// Launch workers
	var wg sync.WaitGroup
	for i := 0; i < config.ConcurrentWorkers; i++ {
		wg.Add(1)
		go stressTestWorker(i, &wg, workChan, resultChan, config)
	}

	// Queue work items
	for i := 0; i < config.TotalRuns; i++ {
		workChan <- i
	}
	close(workChan)

	// Wait for workers to finish
	wg.Wait()
	close(resultChan)

	// Collect results
	results := make([]*StressTestResult, 0, config.TotalRuns)
	for result := range resultChan {
		results = append(results, result)
	}

	duration := time.Since(startTime)

	// Compute summary statistics
	summary := &StressTestSummary{
		Config:           config,
		Results:          results,
		TotalDuration:    duration,
		ComputationStats: computeTimingStats(results, func(r *StressTestResult) float64 { return r.ComputationTime }),
		ProofGenStats:    computeTimingStats(results, func(r *StressTestResult) float64 { return r.ProofGenTime }),
		ProofVerifyStats: computeTimingStats(results, func(r *StressTestResult) float64 { return r.ProofVerifyTime }),
		ProofsPerSecond:  float64(config.TotalRuns) / duration.Seconds(),
		SuccessRate:      calculateSuccessRate(results),
	}

	return summary
}

// stressTestWorker processes stress test work items
func stressTestWorker(id int, wg *sync.WaitGroup, workChan chan int, resultChan chan *StressTestResult, config StressTestConfig) {
	defer wg.Done()

	// Get modulus
	var modulus *big.Int
	if config.ModulusBits > 0 {
		modulus = getPrimeModulus(config.ModulusBits)
	} else {
		modulus = getRSAModulus2048()
	}

	for range workChan {
		result := &StressTestResult{}

		// Calibrate hardware for this worker
		calibrateSteps := int64(1000)
		calibrateConfig := VDFConfig{
			Steps:   calibrateSteps,
			Modulus: big.NewInt(1000000007),
		}
		calibrateVDF := NewVDFWithConfig(calibrateConfig)
		calibrateTime := calibrateVDF.Compute()
		stepsPerSecond := float64(calibrateSteps) / calibrateTime.Seconds()

		// Compute VDF
		totalSteps := int64(math.Round(stepsPerSecond * config.TargetDelaySeconds))
		vdfConfig := VDFConfig{
			Steps:   totalSteps,
			Modulus: modulus,
			Input:   nil,
		}

		vdf := NewVDFWithConfig(vdfConfig)
		computeTime := vdf.Compute()
		result.ComputationTime = computeTime.Seconds()
		result.Steps = vdf.Steps
		result.Success = true

		// Generate Wesolowski proof if requested
		if config.GenerateWesolowski {
			proofStart := time.Now()
			proof := vdf.GenerateWesolowskiProof()
			result.ProofGenTime = time.Since(proofStart).Seconds()

			// Verify proof if requested
			if config.VerifyProofs {
				verifyStart := time.Now()
				if !proof.Verify() {
					result.Success = false
					result.Error = "Wesolowski proof verification failed"
				}
				result.ProofVerifyTime = time.Since(verifyStart).Seconds()
			}
		}

		resultChan <- result
	}
}

// computeTimingStats computes statistics from timing data
func computeTimingStats(results []*StressTestResult, extractor func(*StressTestResult) float64) *TimingStats {
	if len(results) == 0 {
		return nil
	}

	// Extract values
	values := make([]float64, 0, len(results))
	for _, r := range results {
		val := extractor(r)
		if val > 0 {
			values = append(values, val)
		}
	}

	if len(values) == 0 {
		return nil
	}

	// Sort values for percentile calculation
	sortFloats(values)

	// Calculate statistics
	stats := &TimingStats{
		Min:    values[0],
		Max:    values[len(values)-1],
		Mean:   calculateMean(values),
		Median: calculateMedian(values),
		StdDev: calculateStdDev(values),
		P95:    calculatePercentile(values, 0.95),
		P99:    calculatePercentile(values, 0.99),
	}

	return stats
}

// Helper functions for statistics
func sortFloats(values []float64) {
	// Simple bubble sort for small datasets
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func calculateMean(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateMedian(values []float64) float64 {
	if len(values)%2 == 0 {
		return (values[len(values)/2-1] + values[len(values)/2]) / 2
	}
	return values[len(values)/2]
}

func calculateStdDev(values []float64) float64 {
	mean := calculateMean(values)
	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

func calculatePercentile(values []float64, p float64) float64 {
	idx := int(float64(len(values)) * p)
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}

func calculateSuccessRate(results []*StressTestResult) float64 {
	if len(results) == 0 {
		return 0
	}
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	return float64(successCount) / float64(len(results)) * 100
}

// PrintStressTestSummary prints the stress test results
func PrintStressTestSummary(summary *StressTestSummary) {
	fmt.Printf("\n📊 Stress Test Results\n")
	fmt.Printf("======================\n\n")

	fmt.Printf("Total Duration:        %.2f seconds\n", summary.TotalDuration.Seconds())
	fmt.Printf("Success Rate:          %.1f%%\n", summary.SuccessRate)
	fmt.Printf("Throughput:            %.2f proofs/second\n", summary.ProofsPerSecond)

	fmt.Printf("\n⏱️  VDF Computation Timing:\n")
	if summary.ComputationStats != nil {
		printTimingStats("Computation", summary.ComputationStats)
	}

	if summary.Config.GenerateWesolowski {
		fmt.Printf("\n🔐 Wesolowski Proof Generation Timing:\n")
		if summary.ProofGenStats != nil {
			printTimingStats("Proof Gen", summary.ProofGenStats)
		}

		if summary.Config.VerifyProofs {
			fmt.Printf("\n✅ Wesolowski Proof Verification Timing:\n")
			if summary.ProofVerifyStats != nil {
				printTimingStats("Proof Verify", summary.ProofVerifyStats)
			}
		}
	}

	// Print failure details if any
	failures := 0
	for _, r := range summary.Results {
		if !r.Success {
			failures++
		}
	}
	if failures > 0 {
		fmt.Printf("\n❌ Failures: %d\n", failures)
		for i, r := range summary.Results {
			if !r.Success {
				fmt.Printf("   Test %d: %s\n", i, r.Error)
			}
		}
	}
}

func printTimingStats(label string, stats *TimingStats) {
	fmt.Printf("   Min:      %.4f seconds\n", stats.Min)
	fmt.Printf("   Max:      %.4f seconds\n", stats.Max)
	fmt.Printf("   Mean:     %.4f seconds\n", stats.Mean)
	fmt.Printf("   Median:   %.4f seconds\n", stats.Median)
	fmt.Printf("   StdDev:   %.4f seconds\n", stats.StdDev)
	fmt.Printf("   P95:      %.4f seconds\n", stats.P95)
	fmt.Printf("   P99:      %.4f seconds\n", stats.P99)
}

// handleStressTest runs a stress test from command line arguments
func handleStressTest(args []string) {
	stressCmd := flag.NewFlagSet("stress-test", flag.ExitOnError)
	workers := stressCmd.Int("workers", 4, "Number of concurrent workers")
	runs := stressCmd.Int("runs", 10, "Total number of test runs")
	delay := stressCmd.Float64("delay", 5.0, "Target delay in seconds")
	generateWesolowski := stressCmd.Bool("wesolowski", true, "Generate Wesolowski proofs")
	verifyProofs := stressCmd.Bool("verify", true, "Verify Wesolowski proofs")

	stressCmd.Parse(args)

	config := StressTestConfig{
		ConcurrentWorkers:  *workers,
		TotalRuns:          *runs,
		TargetDelaySeconds: *delay,
		ModulusBits:        0, // Use default 2048-bit RSA
		GenerateWesolowski: *generateWesolowski,
		VerifyProofs:       *verifyProofs,
	}

	summary := RunStressTest(config)
	PrintStressTestSummary(summary)

	// Save results to JSON
	resultsFile := "stress_test_results.json"
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Printf("❌ Error marshaling results: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(resultsFile, data, 0644)
	if err != nil {
		fmt.Printf("❌ Error writing results file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n💾 Results saved to %s\n", resultsFile)
}
