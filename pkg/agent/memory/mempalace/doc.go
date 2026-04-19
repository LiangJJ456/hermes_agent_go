// Package mempalace implements a MemPalace-style external memory provider
// for the hermes-agent.
//
// Inspired by github.com/mempalace, it provides:
//   - 4-Layer Memory Stack (Identity → Essential Story → On-Demand → Deep Search)
//   - Knowledge Graph with temporal validity (entities + relationship triples)
//   - Hybrid BM25 + ChromaDB vector semantic search (graceful BM25 fallback)
//   - Automatic session diary with conversation mining
//   - Drawer/Closet architecture (Drawers = raw memories, Closets = index pointers)
//
// Storage: drawers persisted as JSON files under ~/.hermes/palace/drawers/
// Vector index: ChromaDB persistent client at ~/.hermes/palace/chroma/
// Dependencies: github.com/amikos-tech/chroma-go for vector search.
// If ChromaDB is unavailable, the searcher silently degrades to pure BM25.
package mempalace
