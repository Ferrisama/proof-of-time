#!/bin/bash

echo "Proof of Time Travel - Full Pipeline"
echo "========================================"

# Step 1: Run VDF in Go
echo ""
echo "Step 1: Computing VDF in Go..."
cd vdf-go
go run vdf.go > ../output.txt
cd ..

# Step 2: Generate STARK proof with RISC Zero
echo ""
echo "Step 2: Generating STARK proof..."
cd zkvm-risc0/vdf-guest
cargo run --release

echo ""
echo "Pipeline complete!"