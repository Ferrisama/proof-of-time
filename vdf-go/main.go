package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TraceOutput is the JSON structure exported for RISC Zero proof generation
type TraceOutput struct {
	Input     string  `json:"input"`      // Hex or decimal input
	Steps     int64   `json:"steps"`      // Number of squaring iterations
	Modulus   string  `json:"modulus"`    // Hex or decimal modulus
	TimeSpent float64 `json:"time_spent"` // Computation time in seconds
}

func runVDF() {
	fmt.Println("🕐 Proof of Time Travel - VDF with Time Binding")
	fmt.Println("================================================")

	// Calibrate
	fmt.Println("\n📊 Calibrating hardware...")
	calibrateSteps := 10000
	start := time.Now()

	// Simple squaring calibration
	var result uint64 = 12345
	modulus := uint64(1000000007)
	for i := 0; i < calibrateSteps; i++ {
		result = ((result * result) % modulus)
	}

	calibrateTime := time.Since(start)
	stepsPerSecond := float64(calibrateSteps) / calibrateTime.Seconds()

	fmt.Printf("✅ Your hardware: ~%.0f steps/second\n", stepsPerSecond)
	fmt.Printf("   Calibration took: %.3f seconds\n", calibrateTime.Seconds())

	// Target delay
	targetSeconds := 30.0
	totalSteps := int(stepsPerSecond * targetSeconds)

	fmt.Printf("\n⏱️  Target delay: %.0f seconds\n", targetSeconds)
	fmt.Printf("   Required steps: %d\n", totalSteps)
	fmt.Println("\n🔄 Computing VDF...")

	// Actual computation
	start = time.Now()
	result = 12345

	progressInterval := totalSteps / 10
	for i := 0; i < totalSteps; i++ {
		result = ((result * result) % modulus)

		if i > 0 && i%progressInterval == 0 {
			elapsed := time.Since(start).Seconds()
			progress := float64(i) / float64(totalSteps) * 100
			fmt.Printf("   Progress: %.0f%% (%.1fs elapsed)\n", progress, elapsed)
		}
	}

	actualTime := time.Since(start)

	fmt.Printf("\n✅ VDF Complete!\n")
	fmt.Printf("   Actual time: %.2f seconds\n", actualTime.Seconds())
	fmt.Printf("   Target time: %.2f seconds\n", targetSeconds)
	fmt.Printf("   Accuracy: %.1f%%\n", (actualTime.Seconds()/targetSeconds)*100)
	fmt.Printf("   Final output: %d\n", result)

	// Save for RISC Zero
	trace := TraceOutput{
		Input:     "12345",
		Steps:     int64(totalSteps),
		Modulus:   "1000000007",
		TimeSpent: actualTime.Seconds(),
	}

	data, _ := json.MarshalIndent(trace, "", "  ")
	os.WriteFile("../trace.json", data, 0644)

	fmt.Println("\n💾 Trace saved to trace.json")
	fmt.Println("\n🎯 Next: Run 'cargo run --release' in zkvm-risc0/vdf-guest to generate STARK proof")
}

// saveTraceJSON exports VDF computation results to trace.json for RISC Zero
func saveTraceJSON(vdf *VDF) error {
	trace := TraceOutput{
		Input:     vdf.Input.String(), // Decimal representation
		Steps:     vdf.Steps,
		Modulus:   vdf.Modulus.String(), // Decimal representation
		TimeSpent: vdf.Metadata.ComputationTime,
	}

	// Create output directory if needed
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Save to trace.json in parent directory (repo root)
	err = os.WriteFile("../trace.json", data, 0644)
	if err != nil {
		// Try current directory
		err = os.WriteFile("trace.json", data, 0644)
		if err != nil {
			return fmt.Errorf("failed to write trace.json: %w", err)
		}
	}

	return nil
}
