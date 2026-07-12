// Package domain is chiller's shared kernel (U0): the inter-unit contract.
//
// It holds only technology-agnostic types and pure helpers — no I/O, no infra,
// and it imports no other chiller unit (keeps the dependency graph acyclic,
// NFR-02). Every other unit (findings, scan, platform) implements against these.
//
// Two axes are kept structurally separate: a Finding is keyed by CWE (the
// expression axis, FR-14) and carries no PS-ID; Coverage is keyed by PS-ID (the
// accounting axis). The bridge between them (CWE→framework projection) lives in
// the findings unit, not here.
package domain
