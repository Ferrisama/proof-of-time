package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type BenchmarkResult struct {
	TargetDelay    float64 `json:"target_delay_seconds"`
	ActualDelay    float64 `json:"actual_delay_seconds"`
	Steps          int     `json:"steps"`
	StepsPerSecond float64 `json:"steps_per_second"`
	Accuracy       float64 `json:"accuracy_percent"`
	Timestamp      string  `json:"timestamp"`
}

func runBenchmark(targetSeconds float64) BenchmarkResult {
	// Calibrate
	calibrateSteps := 10000
	start := time.Now()
	var result uint64 = 12345
	modulus := uint64(1000000007)

	for i := 0; i < calibrateSteps; i++ {
		result = ((result * result) % modulus)
	}

	calibrateTime := time.Since(start)
	stepsPerSecond := float64(calibrateSteps) / calibrateTime.Seconds()

	// Compute
	totalSteps := int(stepsPerSecond * targetSeconds)
	start = time.Now()
	result = 12345

	for i := 0; i < totalSteps; i++ {
		result = ((result * result) % modulus)
	}

	actualTime := time.Since(start)
	accuracy := (actualTime.Seconds() / targetSeconds) * 100

	return BenchmarkResult{
		TargetDelay:    targetSeconds,
		ActualDelay:    actualTime.Seconds(),
		Steps:          totalSteps,
		StepsPerSecond: stepsPerSecond,
		Accuracy:       accuracy,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
}

func main() {
	// Parse command-line flags
	verifyCmd := flag.NewFlagSet("verify", flag.ExitOnError)
	verifyFile := verifyCmd.String("file", "", "Path to proof file to verify")
	verifyStrict := verifyCmd.Bool("strict", false, "Strict mode (fail on warnings)")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "verify":
			verifyCmd.Parse(os.Args[2:])
			if *verifyFile == "" {
				fmt.Println("❌ Error: --file flag required for verify command")
				os.Exit(1)
			}
			handleVerifyCommand(*verifyFile, *verifyStrict)
			return
		case "analyze":
			if len(os.Args) < 3 {
				fmt.Println("❌ Error: proof file required")
				fmt.Println("Usage: go run . analyze <proof-file>")
				os.Exit(1)
			}
			handleAnalyzeCommand(os.Args[2])
			return
		case "stress-test":
			// Run stress test with command line arguments
			stressCmd := flag.NewFlagSet("stress-test", flag.ExitOnError)
			workers := stressCmd.Int("workers", 4, "Number of concurrent workers")
			runs := stressCmd.Int("runs", 10, "Total number of test runs")
			delay := stressCmd.Float64("delay", 5.0, "Target delay in seconds")
			generateWesolowski := stressCmd.Bool("wesolowski", true, "Generate Wesolowski proofs")
			verifyProofs := stressCmd.Bool("verify", true, "Verify Wesolowski proofs")
			stressCmd.Parse(os.Args[2:])

			config := StressTestConfig{
				ConcurrentWorkers:  *workers,
				TotalRuns:          *runs,
				TargetDelaySeconds: *delay,
				ModulusBits:        0,
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
			return
		case "help", "-h", "--help":
			printHelpMessage()
			return
		}
	}

	// Default: run benchmark dashboard
	runBenchmarkDashboard()
}

// NOTE: Stress test handler is in stress_test.go

func handleVerifyCommand(filePath string, strict bool) {
	fmt.Println("🔍 Verifying Proof")
	fmt.Println("==================")

	proof, err := LoadProof(filePath)
	if err != nil {
		fmt.Printf("❌ Error loading proof: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()

	result := VerifyProof(proof)
	PrintVerificationResult(result)

	if !result.IsValid || (strict && len(result.Warnings) > 0) {
		os.Exit(1)
	}
}

func handleAnalyzeCommand(filePath string) {
	fmt.Println("📊 Proof Analysis")
	fmt.Println("=================")
	fmt.Println()

	proof, err := LoadProof(filePath)
	if err != nil {
		fmt.Printf("❌ Error loading proof: %v\n", err)
		os.Exit(1)
	}

	PrintProofSummary(proof)
	stats := GetProofStats(proof)

	fmt.Println("\n📈 Computed Statistics:")
	fmt.Printf("   Steps/Second:          %.0f\n", stats["steps_per_second"])
	fmt.Printf("   Proof Size (KB):       %.2f\n", stats["proof_size_kb"])
	fmt.Printf("   Target Delay (s):      %.2f\n", stats["target_delay"])
}

func printHelpMessage() {
	fmt.Print(`
🔐 Proof of Time - Command Line Tool
====================================

USAGE:
  go run . [command] [options]

COMMANDS:
  benchmark (default)      Run benchmark dashboard
  verify --file <path>     Verify a proof file
                          Optional: --strict (fail on warnings)
  analyze <path>           Detailed analysis of a proof file
  stress-test [options]    Run stress tests for proof generation
  help                     Show this help message

EXAMPLES:
  go run . benchmark
  go run . verify --file proof_abc123.json
  go run . verify --file proof_abc123.json --strict
  go run . analyze proof_abc123.json
  go run . stress-test --workers 2 --runs 5 --delay 3.0
  go run . help
`)
}

func runBenchmarkDashboard() {
	fmt.Println("📊 Proof of Time - Benchmark Dashboard")
	fmt.Println("======================================")

	delays := []float64{5, 10, 30, 60}
	results := []BenchmarkResult{}

	for _, delay := range delays {
		fmt.Printf("\n🔄 Testing %gs delay...\n", delay)
		result := runBenchmark(delay)
		results = append(results, result)

		fmt.Printf("   ✅ Completed in %.2fs (%.1f%% accurate)\n",
			result.ActualDelay, result.Accuracy)
	}

	// Display results
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("BENCHMARK RESULTS")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("%-12s %-12s %-12s %-12s\n", "Target", "Actual", "Steps", "Accuracy")
	fmt.Println(strings.Repeat("-", 60))

	for _, r := range results {
		fmt.Printf("%-12.1fs %-12.2fs %-12d %-12.1f%%\n",
			r.TargetDelay, r.ActualDelay, r.Steps, r.Accuracy)
	}

	// Save results
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("benchmark_results.json", data, 0644)

	fmt.Println("\n💾 Results saved to benchmark_results.json")
}
