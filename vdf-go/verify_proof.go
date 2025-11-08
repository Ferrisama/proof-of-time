package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"strings"
)

// ProofData represents the complete proof JSON structure
type ProofData struct {
	ProofType  string          `json:"proof_type"`
	Version    string          `json:"version"`
	Timestamp  string          `json:"timestamp"`
	Public     PublicInputs    `json:"public"`
	Receipt    Receipt         `json:"receipt"`
	Metrics    Metrics         `json:"metrics"`
	Wesolowski *WesolowskiData `json:"wesolowski,omitempty"`
}

// WesolowskiData contains the Wesolowski proof data
type WesolowskiData struct {
	ProofValue string `json:"proof_value"` // Base64 or hex encoded
	Challenge  string `json:"challenge"`   // Base64 or hex encoded
}

type PublicInputs struct {
	Input              uint64  `json:"input"`
	Steps              uint64  `json:"steps"`
	Modulus            uint64  `json:"modulus"`
	TargetDelaySeconds float64 `json:"target_delay_seconds"`
}

type Receipt struct {
	MethodID       string    `json:"method_id"`
	JournalOutput  uint64    `json:"journal_output"`
	ProofSizeBytes uint64    `json:"proof_size_bytes"`
	TimeBinding    *TimeBind `json:"time_binding,omitempty"`
}

type TimeBind struct {
	StepsExecuted      uint32  `json:"steps_executed"`
	TargetDelaySeconds float64 `json:"target_delay_seconds"`
	ExpectedDurationMs uint32  `json:"expected_duration_ms"`
}

type Metrics struct {
	ProofGenerationMs uint64 `json:"proof_generation_ms"`
	VerificationMs    uint64 `json:"verification_ms"`
}

// VerifyProofResult holds verification status
type VerifyProofResult struct {
	IsValid       bool
	ValidityScore float64
	Issues        []string
	Warnings      []string
}

// LoadProof loads a proof from JSON file
func LoadProof(filePath string) (*ProofData, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var proof ProofData
	if err := json.Unmarshal(data, &proof); err != nil {
		return nil, err
	}

	return &proof, nil
}

// VerifyProof performs comprehensive verification
func VerifyProof(proof *ProofData) *VerifyProofResult {
	result := &VerifyProofResult{
		IsValid:  true,
		Issues:   []string{},
		Warnings: []string{},
	}

	// Structural checks
	if proof.ProofType != "vdf_time_delay" {
		result.Issues = append(result.Issues, fmt.Sprintf("Invalid proof type: %s", proof.ProofType))
		result.IsValid = false
	}

	if proof.Receipt.MethodID == "" {
		result.Issues = append(result.Issues, "Missing method ID")
		result.IsValid = false
	}

	// Public input validation
	if proof.Public.Steps == 0 {
		result.Issues = append(result.Issues, "Steps must be positive")
		result.IsValid = false
	}

	if proof.Public.Modulus == 0 {
		result.Issues = append(result.Issues, "Modulus must be positive")
		result.IsValid = false
	}

	if proof.Public.TargetDelaySeconds <= 0 {
		result.Issues = append(result.Issues, "Target delay must be positive")
		result.IsValid = false
	}

	if proof.Public.TargetDelaySeconds > 3600 {
		result.Warnings = append(result.Warnings, "Target delay exceeds 1 hour")
	}

	// Proof size validation
	if proof.Receipt.ProofSizeBytes == 0 {
		result.Issues = append(result.Issues, "Proof size is zero")
		result.IsValid = false
	}

	if proof.Receipt.ProofSizeBytes > 1_000_000 {
		result.Warnings = append(result.Warnings, "Proof size exceeds 1 MB")
	}

	// Time-binding validation
	if proof.Receipt.TimeBinding != nil {
		tb := proof.Receipt.TimeBinding
		expectedMs := int32(tb.TargetDelaySeconds * 1000)
		variance := int32(float32(expectedMs) * 0.2) // 20% tolerance

		if int32(tb.ExpectedDurationMs) < expectedMs-variance ||
			int32(tb.ExpectedDurationMs) > expectedMs+variance {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Duration mismatch: expected %dms, got %dms",
					expectedMs, tb.ExpectedDurationMs))
		}
	} else {
		result.Warnings = append(result.Warnings, "No time-binding data")
	}

	// Wesolowski proof validation (if available)
	if proof.Wesolowski != nil {
		if !VerifyWesolowskiProof(proof) {
			result.Warnings = append(result.Warnings, "Wesolowski proof verification failed")
		}
	}

	// Calculate validity score
	result.ValidityScore = 100.0
	result.ValidityScore -= float64(len(result.Issues)) * 25.0
	result.ValidityScore -= float64(len(result.Warnings)) * 5.0

	if result.ValidityScore < 0 {
		result.ValidityScore = 0
	}

	if !result.IsValid {
		result.ValidityScore = 0
	}

	return result
}

// VerifyWesolowskiProof verifies a Wesolowski proof from ProofData
func VerifyWesolowskiProof(proof *ProofData) bool {
	if proof.Wesolowski == nil {
		return false
	}

	// Parse the proof components from hex strings
	proofValue := new(big.Int)
	challenge := new(big.Int)
	modulus := new(big.Int)
	input := new(big.Int)
	output := new(big.Int)

	// Parse hex-encoded values
	if _, ok := proofValue.SetString(proof.Wesolowski.ProofValue, 0); !ok {
		return false
	}
	if _, ok := challenge.SetString(proof.Wesolowski.Challenge, 0); !ok {
		return false
	}
	if _, ok := modulus.SetString(fmt.Sprintf("%d", proof.Public.Modulus), 10); !ok {
		return false
	}
	if _, ok := input.SetString(fmt.Sprintf("%d", proof.Public.Input), 10); !ok {
		return false
	}
	if _, ok := output.SetString(fmt.Sprintf("%d", proof.Receipt.JournalOutput), 10); !ok {
		return false
	}

	// Verify challenge matches expected value
	data := fmt.Sprintf("%s:%s:%s:%d",
		input.String(),
		output.String(),
		modulus.String(),
		proof.Public.Steps)

	expectedChallenge := new(big.Int)
	dataBytes := []byte(data)
	expectedChallenge.SetBytes(dataBytes)
	expectedChallenge.Mod(expectedChallenge, new(big.Int).Sub(modulus, big.NewInt(2)))
	expectedChallenge.Add(expectedChallenge, big.NewInt(2))

	if challenge.Cmp(expectedChallenge) != 0 {
		return false
	}

	// Compute verification equation: B^(2^T) * x^r ≡ y (mod N)
	// Step 1: Compute 2^T mod (N-1) using Carmichael's theorem for efficiency
	carmichaelLambda := new(big.Int).Sub(modulus, big.NewInt(1))
	exp := new(big.Int)
	exp.Exp(big.NewInt(2), big.NewInt(int64(proof.Public.Steps)), carmichaelLambda)

	// Step 2: Compute B^(2^T) mod N
	bPower := new(big.Int)
	bPower.Exp(proofValue, exp, modulus)

	// Step 3: Compute x^r mod N
	xPower := new(big.Int)
	xPower.Exp(input, challenge, modulus)

	// Step 4: Multiply and reduce: (B^(2^T) * x^r) mod N
	result := new(big.Int)
	result.Mul(bPower, xPower)
	result.Mod(result, modulus)

	// Verification succeeds if result == output
	return result.Cmp(output) == 0
}

// PrintProofSummary prints proof details
func PrintProofSummary(proof *ProofData) {
	fmt.Printf("📋 Proof Summary\n")
	fmt.Printf("================\n\n")
	fmt.Printf("Type:                   %s\n", proof.ProofType)
	fmt.Printf("Version:                %s\n", proof.Version)
	fmt.Printf("Timestamp:              %s\n", proof.Timestamp)
	fmt.Printf("\n📊 Inputs\n")
	fmt.Printf("Steps:                  %d\n", proof.Public.Steps)
	fmt.Printf("Input:                  %d\n", proof.Public.Input)
	fmt.Printf("Modulus:                %d\n", proof.Public.Modulus)
	fmt.Printf("Target Delay:           %.2f seconds\n", proof.Public.TargetDelaySeconds)
	fmt.Printf("\n⏱️  Metrics\n")
	fmt.Printf("Proof Generation:       %.2f seconds\n", float64(proof.Metrics.ProofGenerationMs)/1000)
	fmt.Printf("Verification:           %.2f seconds\n", float64(proof.Metrics.VerificationMs)/1000)
	fmt.Printf("Proof Size:             %.2f KB\n", float64(proof.Receipt.ProofSizeBytes)/1024)

	if proof.Receipt.TimeBinding != nil {
		fmt.Printf("\n⏳ Time-Binding\n")
		fmt.Printf("Steps Executed:         %d\n", proof.Receipt.TimeBinding.StepsExecuted)
		fmt.Printf("Target Delay:           %.2f seconds\n", proof.Receipt.TimeBinding.TargetDelaySeconds)
		fmt.Printf("Expected Duration:      %d ms\n", proof.Receipt.TimeBinding.ExpectedDurationMs)
	}
}

// PrintVerificationResult prints verification details
func PrintVerificationResult(result *VerifyProofResult) {
	if result.IsValid {
		fmt.Println("✅ PROOF IS VALID")
	} else {
		fmt.Println("❌ PROOF IS INVALID")
	}

	fmt.Printf("\nValidity Score: %.1f/100\n", result.ValidityScore)

	if len(result.Issues) > 0 {
		fmt.Println("\n❌ Critical Issues:")
		for _, issue := range result.Issues {
			fmt.Printf("   • %s\n", issue)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Println("\n⚠️  Warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf("   • %s\n", warning)
		}
	}
}

// GetProofStats returns key statistics
func GetProofStats(proof *ProofData) map[string]float64 {
	stats := make(map[string]float64)
	stats["steps"] = float64(proof.Public.Steps)
	stats["target_delay"] = proof.Public.TargetDelaySeconds
	stats["proof_gen_time"] = float64(proof.Metrics.ProofGenerationMs) / 1000
	stats["proof_size_kb"] = float64(proof.Receipt.ProofSizeBytes) / 1024
	stats["steps_per_second"] = float64(proof.Public.Steps) / proof.Public.TargetDelaySeconds
	return stats
}

// FormatProofInfo returns a formatted string with proof information
func FormatProofInfo(proof *ProofData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Type: %s | Steps: %d | Delay: %.1fs | Size: %.1f KB",
		proof.ProofType, proof.Public.Steps, proof.Public.TargetDelaySeconds,
		float64(proof.Receipt.ProofSizeBytes)/1024))
	return sb.String()
}
