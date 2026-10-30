package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// This separate immutable supplement repairs R1-QA-1 without replacing or
// editing the original111-case oracle. Every row goes through real production
// Lint and its actual MD091/MD087 callbacks, source mapping and shared controls.
func TestMD091ConfirmedOpaqueRegressionOracle(t *testing.T) {
	raw, err := os.ReadFile("testdata/md091_opaque.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != "6b88cf374a5748621e557a956b7e0ec49231e34a792caecc64dc42caa822e31d" {
		t.Fatal("root supplementary oracle identity", err)
	}
	var oracle struct {
		Cases         []md091Literal
		Profiles      []string
		Count         int    `json:"literal_case_count"`
		Repetitions   int    `json:"planned_case_flavor_repetitions"`
		Original      string `json:"original_oracle_sha256"`
		NotExecutions bool   `json:"not_execution_counts"`
	}
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Cases) != 18 || oracle.Count != 18 || oracle.Repetitions != 36 || !oracle.NotExecutions || !reflect.DeepEqual(oracle.Profiles, []string{"commonmark", "gfm"}) || oracle.Original != "2110047ce76969edbe4970529358d4caef98a4d0021bcff824db3426f013f05d" {
		t.Fatal("supplement metadata", oracle)
	}
	// The shared harness compares the COMPLETE visible MD091/MD087 inventory,
	// all hidden observations, exact messages/marker slices/byte+scalar ranges,
	// audit owner/effect/token/interval/EOF provenance and unchanged source with
	// no edits/documents/previews. No provider result is fabricated or filtered.
	md091CheckProductionLiterals(t, oracle.Cases)
}
