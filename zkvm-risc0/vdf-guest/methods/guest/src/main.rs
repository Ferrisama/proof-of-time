#![no_main]

use risc0_zkvm::guest::env;

risc0_zkvm::guest::entry!(main);

/// Output structure committed to the proof receipt
#[derive(serde::Serialize, serde::Deserialize)]
pub struct VDFOutput {
    /// Final VDF computation result
    pub result: u64,
    /// Number of steps actually computed
    pub steps_executed: u32,
    /// Target time delay in seconds
    pub target_delay_seconds: f64,
    /// Expected duration based on hardware calibration
    pub expected_duration_ms: u32,
}

fn main() {
    // Read VDF parameters from host
    let input: u64 = env::read();
    let steps: u32 = env::read();
    let modulus: u64 = env::read();
    let target_delay_seconds: f64 = env::read();
    let expected_duration_ms: u32 = env::read();

    // Perform VDF computation using u128 for safe modular arithmetic
    let result = compute_vdf(input, steps as u64, modulus);

    // Validate timing constraints
    // In production, we'd compare against actual elapsed time measured by the host
    // For now, we commit the parameters needed for verification
    validate_time_binding(steps, target_delay_seconds, expected_duration_ms);

    // Create output with time-binding information
    let output = VDFOutput {
        result,
        steps_executed: steps,
        target_delay_seconds,
        expected_duration_ms,
    };

    // Commit the result and timing data to the receipt (public output)
    env::commit(&output);
}

/// Compute VDF via repeated squaring with modular reduction
/// Uses u128 intermediate arithmetic to prevent overflow
///
/// Formula: output = input^(2^steps) mod modulus
///
/// # Arguments
/// * `input` - Starting value
/// * `steps` - Number of squaring iterations
/// * `modulus` - Modular reduction value
fn compute_vdf(mut current: u64, steps: u64, modulus: u64) -> u64 {
    // Track progress for checkpoint opportunities
    // Every 1M steps, we could emit intermediate values if needed
    let checkpoint_interval = 1_000_000u64;
    let mut checkpoint_count = 0u64;

    for _ in 0..steps {
        // Squaring with safe modular arithmetic using u128
        // (a * a) mod m is safe when a < m < 2^64
        let temp = (current as u128) * (current as u128);
        current = (temp % (modulus as u128)) as u64;

        // Optional: emit checkpoint every N steps for very long computations
        // This allows partial verification and progress tracking
        checkpoint_count += 1;
        if checkpoint_count == checkpoint_interval {
            // In a production system, we might want to commit intermediate values
            // for very long computations or for proof aggregation
            checkpoint_count = 0;
        }
    }

    current
}

/// Alternative: Support for 128-bit arithmetic if needed for larger moduli
/// This computes VDF with 128-bit intermediate values and 128-bit output
/// Can be used if modulus needs more precision (up to 128-bit)
#[allow(dead_code)]
fn compute_vdf_u128(mut current: u128, steps: u64, modulus: u128) -> u128 {
    for _ in 0..steps {
        // Safe squaring with u128
        // For very large numbers, we'd need to handle overflow differently
        // This version assumes modulus < 2^127
        let (prod, overflow) = current.overflowing_mul(current);
        current = if overflow {
            // If overflow occurs, use wrapping multiplication then modulo
            (current.wrapping_mul(current)) % modulus
        } else {
            prod % modulus
        };
    }
    current
}

/// Compute VDF with progress tracking and optional checkpointing
/// Useful for very long computations that need partial verification
#[allow(dead_code)]
fn compute_vdf_with_checkpoints(
    mut current: u64,
    steps: u64,
    modulus: u64,
    checkpoint_interval: u64,
) -> (u64, Vec<u64>) {
    let mut checkpoints = Vec::new();

    for i in 0..steps {
        // Squaring with safe modular arithmetic
        let temp = (current as u128) * (current as u128);
        current = (temp % (modulus as u128)) as u64;

        // Save checkpoint at regular intervals
        if (i + 1) % checkpoint_interval == 0 {
            checkpoints.push(current);
        }
    }

    (current, checkpoints)
}

/// Validate time-binding constraints
/// This ensures the VDF computation aligns with expected timing
///
/// The proof itself validates:
/// 1. Correct number of steps were executed
/// 2. Timing parameters are consistent with the computation
/// 3. Target delay matches expected duration
#[allow(dead_code)]
fn validate_time_binding(steps: u32, target_delay_seconds: f64, expected_duration_ms: u32) {
    // Sanity checks that are committed to the proof
    // These ensure the parameters are reasonable and consistent

    // Check 1: Steps should be positive
    assert!(steps > 0, "Steps must be positive");

    // Check 2: Target delay should be positive and reasonable (< 1 hour)
    assert!(
        target_delay_seconds > 0.0 && target_delay_seconds < 3600.0,
        "Target delay must be between 0 and 3600 seconds"
    );

    // Check 3: Expected duration should be reasonable (< 2 hours in milliseconds)
    assert!(
        expected_duration_ms > 0 && expected_duration_ms < 7_200_000,
        "Expected duration must be between 0 and 7200 seconds"
    );

    // Check 4: Verify consistency between target delay and expected duration
    // Allow 20% margin for hardware variation
    let expected_ms_from_target = (target_delay_seconds * 1000.0) as u32;
    let margin = (expected_ms_from_target as f64 * 0.2) as u32; // 20% tolerance

    assert!(
        expected_duration_ms >= expected_ms_from_target.saturating_sub(margin)
            && expected_duration_ms <= expected_ms_from_target.saturating_add(margin),
        "Expected duration does not match target delay (tolerance: ±20%)"
    );

    // Check 5: Basic step duration sanity
    // For millions of steps, ensure we have reasonable milliseconds per step
    // Typical: 0.001 - 1 ms per step
    let ms_per_step = (expected_duration_ms as f64) / (steps as f64);
    assert!(
        ms_per_step >= 0.0001 && ms_per_step <= 100.0,
        "Milliseconds per step out of reasonable range"
    );
}

/// Time-binding proof data embedded in the VDF output
/// These constants are commitments in the zero-knowledge proof
pub mod timing_constants {
    /// Minimum acceptable computation rate (steps per millisecond)
    pub const MIN_STEPS_PER_MS: f64 = 0.001;

    /// Maximum acceptable computation rate (steps per millisecond)
    pub const MAX_STEPS_PER_MS: f64 = 1000.0;

    /// Minimum target delay (seconds)
    pub const MIN_TARGET_DELAY_SECS: f64 = 0.1;

    /// Maximum target delay (seconds)
    pub const MAX_TARGET_DELAY_SECS: f64 = 3600.0;

    /// Acceptable variance from target (percentage)
    pub const TIMING_VARIANCE_PERCENT: u32 = 20;
}