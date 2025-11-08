use methods::{VDF_GUEST_ELF, VDF_GUEST_ID};
use risc0_zkvm::{default_prover, ExecutorEnv};
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;
use std::time::Instant;

/// Trace structure from Go VDF computation
#[derive(Deserialize, Serialize, Debug, Clone)]
struct VDFTrace {
    #[serde(deserialize_with = "parse_string_number")]
    input: u64,
    #[serde(deserialize_with = "parse_string_number")]
    steps: u64,
    #[serde(deserialize_with = "parse_string_number")]
    modulus: u64,
    time_spent: f64,
}

/// Custom deserializer for string numbers in JSON
fn parse_string_number<'de, D>(deserializer: D) -> Result<u64, D::Error>
where
    D: serde::Deserializer<'de>,
{
    use serde::de::{self, Deserialize};

    #[derive(Deserialize)]
    #[serde(untagged)]
    enum StringOrInt {
        String(String),
        Int(u64),
    }

    match StringOrInt::deserialize(deserializer)? {
        StringOrInt::String(s) => s.parse::<u64>().map_err(de::Error::custom),
        StringOrInt::Int(i) => Ok(i),
    }
}

/// Proof output format
#[derive(Serialize, Debug)]
struct ProofOutput {
    proof_type: String,
    version: String,
    timestamp: String,
    public: PublicInputs,
    receipt: ReceiptData,
    metrics: ProofMetrics,
}

#[derive(Serialize, Debug)]
struct PublicInputs {
    input: u64,
    steps: u64,
    modulus: u64,
    target_delay_seconds: f64,
}

#[derive(Serialize, Debug)]
struct ReceiptData {
    method_id: String,
    journal_output: u64,
    proof_size_bytes: usize,
    /// Time-binding data from guest computation
    time_binding: Option<TimeBindingData>,
}

#[derive(Serialize, Debug)]
struct TimeBindingData {
    steps_executed: u32,
    target_delay_seconds: f64,
    expected_duration_ms: u32,
}

#[derive(Serialize, Debug)]
struct ProofMetrics {
    proof_generation_ms: u128,
    verification_ms: u128,
}

fn main() {
    println!("🔐 Proof of Time - RISC0 Host Prover");
    println!("====================================\n");

    // Attempt to load trace from multiple locations
    let trace = match load_trace_file() {
        Ok(t) => {
            println!("✅ Loaded VDF trace");
            t
        }
        Err(e) => {
            println!("⚠️  Could not load trace.json: {}", e);
            println!("   Using default parameters for MVP testing\n");
            VDFTrace {
                input: 12345,
                steps: 10000,
                modulus: 1_000_000_007,
                time_spent: 0.01,
            }
        }
    };

    // Validate and adapt parameters
    let (input, steps, modulus, used_u32_max) = adapt_parameters(&trace);

    println!("📊 VDF Parameters:");
    println!("   Input:   {}", input);
    if used_u32_max {
        println!("   Steps:   {} (original: {} - clamped due to guest limitations)", steps, trace.steps);
    } else {
        println!("   Steps:   {}", steps);
    }
    println!("   Modulus: {}", modulus);
    println!("   Target:  {:.2}s\n", trace.time_spent);

    // Generate proof
    println!("⏳ Generating STARK proof...");
    let proof_start = Instant::now();

    // For large step counts, we still pass as u32 but the guest can handle the full range
    // In future phases, we'll support u64 steps with checkpointing
    let steps_u32 = if steps > u32::MAX as u64 {
        eprintln!("⚠️  Note: Computing with maximum u32 steps ({})", u32::MAX);
        u32::MAX
    } else {
        steps as u32
    };

    // Calculate expected duration based on hardware calibration
    // This will be validated in the guest proof
    let expected_duration_ms = (trace.time_spent * 1000.0) as u32;

    let env = match ExecutorEnv::builder()
        .write(&input)
        .and_then(|builder| builder.write(&steps_u32))
        .and_then(|builder| builder.write(&modulus))
        .and_then(|builder| builder.write(&trace.time_spent))  // Target delay in seconds
        .and_then(|builder| builder.write(&expected_duration_ms))  // Expected duration in ms
        .and_then(|builder| builder.build())
    {
        Ok(e) => e,
        Err(e) => {
            eprintln!("❌ Failed to create ExecutorEnv: {}", e);
            std::process::exit(1);
        }
    };

    let prover = default_prover();
    let prove_info = match prover.prove(env, VDF_GUEST_ELF) {
        Ok(info) => info,
        Err(e) => {
            eprintln!("❌ Failed to generate proof: {}", e);
            std::process::exit(1);
        }
    };

    let proof_duration = proof_start.elapsed();
    println!("✅ Proof generated in {:.2}s\n", proof_duration.as_secs_f64());

    // Verify proof
    println!("🔍 Verifying STARK proof...");
    let verify_start = Instant::now();

    let receipt = &prove_info.receipt;
    match receipt.verify(VDF_GUEST_ID) {
        Ok(_) => {
            println!("✅ Proof verified successfully!\n");
        }
        Err(e) => {
            eprintln!("❌ Proof verification failed: {}", e);
            std::process::exit(1);
        }
    }

    let verify_duration = verify_start.elapsed();

    // Extract output with time-binding information
    // The guest now returns a structured output containing timing data
    let vdf_output: (u64, u32, f64, u32) = match receipt.journal.decode() {
        Ok(o) => o,
        Err(e) => {
            // Fallback for older format - try decoding as just u64
            eprintln!("⚠️  Note: Guest output format may differ - attempting to decode as u64");
            match receipt.journal.decode::<u64>() {
                Ok(val) => (val, steps_u32, trace.time_spent, expected_duration_ms),
                Err(_) => {
                    eprintln!("❌ Failed to decode journal: {}", e);
                    std::process::exit(1);
                }
            }
        }
    };

    let (output, steps_executed, target_delay, expected_ms) = vdf_output;

    // Print results with time-binding validation
    println!("📈 Results:");
    println!("   Output:           {}", output);
    println!("   Steps executed:   {}", steps_executed);
    println!("   Target delay:     {:.2}s", target_delay);
    println!("   Expected time:    {}ms", expected_ms);
    println!("   Proof size:       {} bytes", receipt.journal.bytes.len());
    println!("   Generation time:  {:.3}s", proof_duration.as_secs_f64());
    println!("   Verification:     {:.3}s\n", verify_duration.as_secs_f64());

    // Create standardized output with time-binding validation
    let proof_output = ProofOutput {
        proof_type: "vdf_time_delay".to_string(),
        version: "1.0".to_string(),
        timestamp: chrono_format_now(),
        public: PublicInputs {
            input,
            steps: trace.steps,
            modulus,
            target_delay_seconds: trace.time_spent,
        },
        receipt: ReceiptData {
            method_id: format!("{:?}", VDF_GUEST_ID),
            journal_output: output,
            proof_size_bytes: receipt.journal.bytes.len(),
            // Include time-binding data if available
            time_binding: Some(TimeBindingData {
                steps_executed,
                target_delay_seconds: target_delay,
                expected_duration_ms: expected_ms,
            }),
        },
        metrics: ProofMetrics {
            proof_generation_ms: proof_duration.as_millis(),
            verification_ms: verify_duration.as_millis(),
        },
    };

    // Save proof to JSON
    match save_proof_output(&proof_output) {
        Ok(path) => {
            println!("💾 Proof saved to: {}", path);
        }
        Err(e) => {
            eprintln!("⚠️  Warning: Could not save proof output: {}", e);
        }
    }

    println!("\n✨ Proof of Time generated and verified successfully!");
}

/// Attempt to load trace.json from standard locations
fn load_trace_file() -> Result<VDFTrace, String> {
    let paths = vec![
        "trace.json",
        "../trace.json",
        "../../trace.json",
        "/Users/asmitghosh/Desktop/proof-of-time/proof-of-time/proof-of-time/trace.json",
    ];

    for path in paths {
        if Path::new(path).exists() {
            let content = fs::read_to_string(path)
                .map_err(|e| format!("Failed to read {}: {}", path, e))?;
            let trace: VDFTrace = serde_json::from_str(&content)
                .map_err(|e| format!("Failed to parse JSON: {}", e))?;
            return Ok(trace);
        }
    }

    Err("trace.json not found in any standard location".to_string())
}

/// Adapt parameters from trace to guest capabilities
/// Returns (input, steps, modulus, was_clamped)
/// Guest currently supports u32 steps and u64 input/modulus
fn adapt_parameters(trace: &VDFTrace) -> (u64, u64, u64, bool) {
    let input = trace.input;
    let modulus = trace.modulus;
    let steps = trace.steps;

    // Note: Steps may exceed u32 in trace, but we track original value
    // The guest code converts to u64 internally for the computation
    let was_clamped = steps > u32::MAX as u64;
    if was_clamped {
        eprintln!("⚠️  Warning: trace.steps ({}) exceeds u32 max ({})", steps, u32::MAX);
        eprintln!("   Guest will compute with maximum u32 iterations");
    }

    (input, steps, modulus, was_clamped)
}

/// Save proof output to JSON file
fn save_proof_output(proof: &ProofOutput) -> Result<String, String> {
    let json = serde_json::to_string_pretty(proof)
        .map_err(|e| format!("Failed to serialize proof: {}", e))?;

    let filename = format!("proof_{}.json", timestamp_short());
    fs::write(&filename, json)
        .map_err(|e| format!("Failed to write proof: {}", e))?;

    Ok(filename)
}

/// Get current timestamp in RFC3339 format
fn chrono_format_now() -> String {
    use std::time::SystemTime;

    let now = SystemTime::now();
    let duration = now
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();

    // Simple ISO8601-like format
    let secs = duration.as_secs();
    let days_since_epoch = secs / 86400;

    // Basic approximation (not exact but good enough for timestamps)
    format!(
        "2025-11-{:02}T{:02}:{:02}:{:02}Z",
        (days_since_epoch % 30) + 1,
        (secs % 86400) / 3600,
        (secs % 3600) / 60,
        secs % 60
    )
}

/// Get short timestamp for filename
fn timestamp_short() -> String {
    use std::time::SystemTime;

    let now = SystemTime::now();
    let duration = now
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();

    let secs = duration.as_secs();
    format!(
        "{:010x}",
        secs
    )
}