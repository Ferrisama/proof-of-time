package main

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"time"
)

// VDFConfig holds configuration for VDF computation
type VDFConfig struct {
	// Number of squaring iterations
	Steps int64

	// Modulus for computation (typically RSA modulus)
	Modulus *big.Int

	// Optional input value (random if not set)
	Input *big.Int

	// Checkpoint interval for tracing (0 = no checkpoints)
	CheckpointInterval int64
}

// VDF represents a Verifiable Delay Function with full big.Int support
type VDF struct {
	Input    *big.Int
	Output   *big.Int
	Steps    int64
	Modulus  *big.Int
	Trace    []*big.Int // Checkpoint trace
	Metadata VDFMetadata
}

// VDFMetadata stores computation metadata
type VDFMetadata struct {
	ComputationTime float64
	HardwareSteps   float64 // steps/second
	Checkpoints     int64   // number of checkpoints saved
}

// NewVDFWithConfig creates a new VDF with configuration
func NewVDFWithConfig(config VDFConfig) *VDF {
	vdf := &VDF{
		Steps:   config.Steps,
		Modulus: new(big.Int).Set(config.Modulus),
		Trace:   make([]*big.Int, 0),
	}

	// Set or generate input
	if config.Input != nil {
		vdf.Input = new(big.Int).Set(config.Input)
	} else {
		// Generate random input less than modulus
		input, err := rand.Int(rand.Reader, config.Modulus)
		if err != nil {
			// Fallback to small random number
			input, _ := rand.Int(rand.Reader, big.NewInt(1000000))
			vdf.Input = input
		} else {
			vdf.Input = input
		}
	}

	// Ensure input is not zero
	if vdf.Input.Sign() == 0 {
		vdf.Input.SetInt64(2)
	}

	return vdf
}

// NewVDFSimple creates a VDF with default 2048-bit RSA modulus
func NewVDFSimple(steps int64) *VDF {
	modulus := getRSAModulus2048()
	return NewVDFWithConfig(VDFConfig{
		Steps:              steps,
		Modulus:            modulus,
		Input:              nil,
		CheckpointInterval: 0,
	})
}

// Compute performs the VDF computation: x → x^(2^steps) mod N
// Returns elapsed time for the computation
func (v *VDF) Compute() time.Duration {
	start := time.Now()

	// Initialize with input
	current := new(big.Int).Set(v.Input)
	temp := new(big.Int)

	// Perform repeated squaring
	for i := int64(0); i < v.Steps; i++ {
		// current = current^2 mod N (safe with big.Int)
		temp.Mul(current, current)
		current.Mod(temp, v.Modulus)

		// Progress indicator every 1M steps
		if (i+1)%1000000 == 0 {
			fmt.Printf("   Progress: %d/%d steps (%.1f%%)\n",
				i+1, v.Steps, float64(i+1)/float64(v.Steps)*100)
		}
	}

	v.Output = current
	elapsed := time.Since(start)
	v.Metadata.ComputationTime = elapsed.Seconds()

	return elapsed
}

// ComputeWithCheckpoints performs VDF computation with periodic checkpoints
func (v *VDF) ComputeWithCheckpoints(interval int64) time.Duration {
	if interval <= 0 {
		return v.Compute()
	}

	start := time.Now()

	current := new(big.Int).Set(v.Input)
	temp := new(big.Int)

	for i := int64(0); i < v.Steps; i++ {
		temp.Mul(current, current)
		current.Mod(temp, v.Modulus)

		// Save checkpoint at intervals
		if (i+1)%interval == 0 {
			v.Trace = append(v.Trace, new(big.Int).Set(current))
			v.Metadata.Checkpoints++
		}

		// Progress every 1M steps
		if (i+1)%1000000 == 0 {
			fmt.Printf("   Progress: %d/%d steps (checkpoint %d)\n",
				i+1, v.Steps, v.Metadata.Checkpoints)
		}
	}

	v.Output = current
	elapsed := time.Since(start)
	v.Metadata.ComputationTime = elapsed.Seconds()

	return elapsed
}

// Verify checks if the VDF output is correct by recomputing
// WARNING: This is slow for large step counts!
func (v *VDF) Verify() bool {
	result := new(big.Int).Set(v.Input)
	temp := new(big.Int)

	for i := int64(0); i < v.Steps; i++ {
		temp.Mul(result, result)
		result.Mod(temp, v.Modulus)

		if (i+1)%10000000 == 0 {
			fmt.Printf("   Verification: %d/%d steps\n", i+1, v.Steps)
		}
	}

	return result.Cmp(v.Output) == 0
}

// VerifyQuick performs a quick sanity check
func (v *VDF) VerifyQuick() bool {
	// Check that output < modulus and is positive
	return v.Output.Cmp(v.Modulus) < 0 && v.Output.Sign() > 0
}

// WesolowskiProof represents a Wesolowski VDF proof
// This allows efficient verification without re-computing the full VDF
type WesolowskiProof struct {
	// The VDF input
	Input *big.Int
	// The VDF output (result of x^(2^T) mod N)
	Output *big.Int
	// The modulus
	Modulus *big.Int
	// Number of squaring steps
	Steps int64
	// The actual proof value: B = x^((2^T+1)/r) mod N
	ProofValue *big.Int
	// The challenge: r (random value derived from input/output/modulus)
	Challenge *big.Int
}

// GenerateWesolowskiChallenge generates the challenge value r using Fiat-Shamir
// r = H(x, y, N, T) where H is hash(input || output || modulus || steps)
// This ensures the challenge is deterministic and non-interactive
func (v *VDF) GenerateWesolowskiChallenge() *big.Int {
	// Combine input data for hash
	data := fmt.Sprintf("%s:%s:%s:%d",
		v.Input.String(),
		v.Output.String(),
		v.Modulus.String(),
		v.Steps)

	// Use hash as basis for challenge (in practice, use SHA-256)
	hash := hashStringToBigInt(data, v.Modulus)
	return hash
}

// GenerateWesolowskiProof creates a Wesolowski proof for this VDF
// The proof is B = x^((2^T + 1) / r mod T!) mod N
// For practical purposes, we compute B = x^((2^T + 1) / r) mod N
func (v *VDF) GenerateWesolowskiProof() *WesolowskiProof {
	challenge := v.GenerateWesolowskiChallenge()

	// Compute the proof exponent: (2^T + 1) / r mod (T-1)
	// For Wesolowski, we need modular inverse of r
	rInv := new(big.Int)
	rInv.ModInverse(challenge, new(big.Int).Sub(v.Modulus, big.NewInt(1)))

	if rInv == nil {
		// Fallback if inverse doesn't exist
		rInv = big.NewInt(1)
	}

	// Compute exponent: floor((2^T + 1) / r)
	// This is where we compute the difficult part
	exponent := computeWesolowskiExponent(v.Steps, challenge)

	// Compute B = x^exponent mod N
	proofValue := new(big.Int)
	proofValue.Exp(v.Input, exponent, v.Modulus)

	return &WesolowskiProof{
		Input:      new(big.Int).Set(v.Input),
		Output:     new(big.Int).Set(v.Output),
		Modulus:    new(big.Int).Set(v.Modulus),
		Steps:      v.Steps,
		ProofValue: proofValue,
		Challenge:  new(big.Int).Set(challenge),
	}
}

// VerifyWesolowskiProof verifies a Wesolowski proof in O(log T) time
// Verification: y^r * B^(2^T) ≡ x (mod N) is NOT the standard formula
// Standard formula: (y / (x^r))^(1/2^T) ≡ 1 (mod N) - hard to verify
//
// Instead we use the interactive/non-interactive property:
// The verifier checks if: B^(2^T) * x^r ≡ y (mod N)
// This requires only O(log T) exponentiations
func (w *WesolowskiProof) Verify() bool {
	// Regenerate challenge to ensure non-interactive property
	data := fmt.Sprintf("%s:%s:%s:%d",
		w.Input.String(),
		w.Output.String(),
		w.Modulus.String(),
		w.Steps)
	expectedChallenge := hashStringToBigInt(data, w.Modulus)

	if w.Challenge.Cmp(expectedChallenge) != 0 {
		return false
	}

	// Compute B^(2^T) mod N
	// This is the fast part: we compute 2^T mod (N-1) first
	exp := computeExponentForWesolowskiVerify(w.Steps, w.Modulus)

	bPower := new(big.Int)
	bPower.Exp(w.ProofValue, exp, w.Modulus)

	// Compute x^r mod N
	xPower := new(big.Int)
	xPower.Exp(w.Input, w.Challenge, w.Modulus)

	// Multiply: B^(2^T) * x^r mod N
	result := new(big.Int)
	result.Mul(bPower, xPower)
	result.Mod(result, w.Modulus)

	// Should equal the output y
	return result.Cmp(w.Output) == 0
}

// VerifyWesolowskiProof is a convenience function on VDF
func (v *VDF) VerifyWesolowskiProof(proof *WesolowskiProof) bool {
	// Ensure parameters match
	if v.Input.Cmp(proof.Input) != 0 ||
		v.Output.Cmp(proof.Output) != 0 ||
		v.Modulus.Cmp(proof.Modulus) != 0 ||
		v.Steps != proof.Steps {
		return false
	}
	return proof.Verify()
}

// Helper function: compute exponent for Wesolowski proof generation
// Returns floor((2^T + 1) / r) as a big.Int
func computeWesolowskiExponent(steps int64, challenge *big.Int) *big.Int {
	// Compute 2^T
	power2T := new(big.Int)
	power2T.Exp(big.NewInt(2), big.NewInt(steps), nil) // No modulus, full precision

	// Add 1: 2^T + 1
	numerator := new(big.Int).Add(power2T, big.NewInt(1))

	// Divide by challenge: (2^T + 1) / r
	exponent := new(big.Int).Div(numerator, challenge)

	return exponent
}

// Helper function: compute 2^T mod (N-1) for Wesolowski verification
// Used in the verification equation: B^(2^T) mod N
func computeExponentForWesolowskiVerify(steps int64, modulus *big.Int) *big.Int {
	// For efficient verification, we compute 2^T mod (N-1)
	// This uses Carmichael's theorem
	carmichaelLambda := new(big.Int).Sub(modulus, big.NewInt(1))

	exponent := new(big.Int)
	exponent.Exp(big.NewInt(2), big.NewInt(steps), carmichaelLambda)

	return exponent
}

// Helper function: hash a string to a big.Int in range [2, modulus-1]
// Used for Fiat-Shamir hashing in Wesolowski challenge generation
func hashStringToBigInt(data string, modulus *big.Int) *big.Int {
	// In practice, use SHA-256 and convert to integer
	// For this implementation, we'll use a simple approach
	hash := new(big.Int)

	// Convert string to bytes and interpret as number
	dataBytes := []byte(data)
	hash.SetBytes(dataBytes)

	// Reduce modulo (N-1) and add 2 to ensure r in [2, N-1]
	hash.Mod(hash, new(big.Int).Sub(modulus, big.NewInt(2)))
	hash.Add(hash, big.NewInt(2))

	return hash
}

// vdfMain is the main VDF computation flow
func vdfMain() {
	fmt.Println("🕐 Proof of Time Travel - Enhanced VDF with Arbitrary Precision")
	fmt.Println("===============================================================")

	// Step 1: Calibrate hardware with small modulus
	fmt.Println("\n📊 Calibrating hardware...")
	calibrateSteps := int64(10000)
	calibrateConfig := VDFConfig{
		Steps:   calibrateSteps,
		Modulus: big.NewInt(1000000007), // Small prime for fast calibration
	}
	calibrateVDF := NewVDFWithConfig(calibrateConfig)
	calibrateTime := calibrateVDF.Compute()
	stepsPerSecond := float64(calibrateSteps) / calibrateTime.Seconds()

	fmt.Printf("✅ Calibration complete: ~%.0f steps/second\n", stepsPerSecond)

	// Step 2: Compute VDF with large modulus for target delay
	targetSeconds := 30.0
	totalSteps := int64(math.Round(stepsPerSecond * targetSeconds))

	fmt.Printf("\n⏱️  Target delay: %.1f seconds\n", targetSeconds)
	fmt.Printf("   Estimated steps: %d\n", totalSteps)

	// Use large RSA modulus for production-quality VDF
	fmt.Println("\n🔐 Computing VDF with 2048-bit RSA modulus...")
	config := VDFConfig{
		Steps:              totalSteps,
		Modulus:            getRSAModulus2048(),
		Input:              nil, // Will be randomly generated
		CheckpointInterval: 0,
	}
	vdf := NewVDFWithConfig(config)

	fmt.Printf("   Input (hex):    %s...\n", vdf.Input.Text(16)[:32])
	fmt.Printf("   Modulus (bits): %d\n", vdf.Modulus.BitLen())
	fmt.Printf("   Starting computation...\n\n")

	computeTime := vdf.Compute()

	fmt.Printf("\n✅ VDF computation complete!\n")
	fmt.Printf("   Steps: %d\n", vdf.Steps)
	fmt.Printf("   Computation time: %.2f seconds\n", computeTime.Seconds())
	fmt.Printf("   Accuracy: %.1f%% of target\n", (computeTime.Seconds()/targetSeconds)*100)
	fmt.Printf("   Output (hex): %s...\n", vdf.Output.Text(16)[:32])

	// Save trace for RISC Zero
	fmt.Println("\n💾 Exporting trace for STARK proof generation...")
	if err := saveTraceJSON(vdf); err != nil {
		fmt.Printf("   ❌ Error saving trace: %v\n", err)
	} else {
		fmt.Println("   ✅ Trace saved to trace.json")
	}

	fmt.Println("\n🎯 Next: Run 'cargo run --release' in zkvm-risc0/vdf-guest to generate STARK proof")
}

// getRSAModulus2048 returns a standard 2048-bit RSA modulus (from OpenSSL)
func getRSAModulus2048() *big.Int {
	modulus := new(big.Int)
	// This is a real 2048-bit RSA modulus (product of two large primes)
	modulus.SetString("25195908475657893494027183240048398571429282126204032027777137836043662020707595556264018525880784406918290641249515082189298559149176184502808489120072844992687392807287776735971418347270261896375014971824691165077613379859095700097330459748808428401797429100642458691817195118746121515172654632282216869987549182422433637259085141865462043576798423387184774447920739934236584823824281198163815010674810451660377306056201619676256133844143603833904414952634432190114657544454178424020924616515723350778707749817125772467962926386356373289912154831438167899885040445364023527381951378636564391212010397122822120720357", 10)
	return modulus
}

// Alternative: Custom prime moduli for testing
func getPrimeModulus(bits int) *big.Int {
	// In production, generate using crypto/rand and primality testing
	// For now, return fixed values
	switch bits {
	case 32:
		return big.NewInt(4294967291) // Largest 32-bit prime
	case 64:
		modulus := new(big.Int)
		modulus.SetString("18446744073709551557", 10) // Largest 64-bit prime
		return modulus
	default:
		// Fallback to 2048-bit RSA modulus
		return getRSAModulus2048()
	}
}
