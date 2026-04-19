# Hermes Agent Go

A production-grade AI agent framework written in Go, featuring a multi-layer memory architecture (MemPalace), hybrid vector + BM25 search, MCP (Model Context Protocol) integration, and a parallel tool execution engine.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Environment Variables](#environment-variables)
- [REPL Commands](#repl-commands)
- [Project Structure](#project-structure)
- [Technical Principles](#technical-principles)
  - [Agent Loop](#agent-loop)
  - [MemPalace 4-Layer Memory](#mempalace-4-layer-memory)
  - [Hybrid Search Engine](#hybrid-search-engine)
  - [Knowledge Graph](#knowledge-graph)
  - [MCP Integration](#mcp-integration)
  - [Tool System](#tool-system)
- [Configuration](#configuration)
- [License](#license)

---

## Architecture Overview

```
+-------------------------------------------------------+
|                    REPL / CLI                          |
|              cmd/hermes/main.go                       |
+------------------------+------------------------------+
                         |
+------------------------v------------------------------+
|                   AIAgent                             |
|  +-------------+ +--------------+ +---------------+  |
|  | Model Router| | Tool Registry| | Budget Tracker|  |
|  | (OpenAI/...)| | (Builtin+MCP)| | (90 iters)   |  |
|  +------+------+ +------+-------+ +---------------+  |
|         |               |                             |
|  +------v---------------v------------------------+   |
|  |           Parallel Tool Executor              |   |
|  |         (max 8 concurrent calls)              |   |
|  +-----------------------------------------------+   |
|                                                       |
|  +-----------------------------------------------+   |
|  |            Memory Manager                     |   |
|  |  +-----------------+  +--------------------+  |   |
|  |  | BuiltinProvider |  | MemPalace Provider |  |   |
|  |  |  (flat files)   |  |  (4-layer stack)   |  |   |
|  |  +-----------------+  +--------+-----------+  |   |
|  +------------------------------------+-----------+   |
|                                       |               |
|  +------------------------------------v----------+   |
|  |              MemPalace                        |   |
|  |  L0 Identity -> L1 Essential -> L2 On-Demand  |   |
|  |                                  -> L3 Search  |   |
|  |  +------------+  +--------------------------+ |   |
|  |  | Knowledge  |  | Hybrid Searcher          | |   |
|  |  | Graph (KG) |  | BM25 + ChromaDB Vectors  | |   |
|  |  +------------+  +--------------------------+ |   |
|  +-----------------------------------------------+   |
+-------------------------------------------------------+
```

---

## Prerequisites

| Dependency | Version | Required |
|---|---|---|
| **Go** | >= 1.22 | Yes |
| **OpenAI-compatible API** | -- | Yes (need API key) |
| **ChromaDB** | >= 0.4 | No, Optional (enables vector search) |

> **ChromaDB** is optional. Without it, the MemPalace searcher gracefully falls back to pure BM25 keyword search. To enable vector semantic search, run a local ChromaDB instance (default `http://localhost:8000`).

---

## Quick Start

### 1. Clone and Build

```bash
git clone git@github.com:LiangJJ456/hermes_agent_go.git
cd hermes_agent_go

go build -o hermes ./cmd/hermes
```

### 2. Configure Environment

```bash
# Required: your OpenAI-compatible API key
export OPENAI_API_KEY="sk-..."

# Optional: custom API endpoint (for Azure, local LLM, etc.)
export OPENAI_BASE_URL="https://api.openai.com/v1"

# Optional: model override (default: openai/gpt-4o)
export HERMES_MODEL="openai/gpt-4o"
```

### 3. Run

```bash
./hermes
```

You will see:

```
Hermes Agent (Go) -- type /quit to exit
   Model: openai/gpt-4o | Budget: 90 iterations
   Memory: /Users/you/.hermes/memories

>>>
```

### 4. (Optional) Start ChromaDB for Vector Search

```bash
# Using Docker
docker run -d -p 8000:8000 chromadb/chroma:latest

# Or via pip
pip install chromadb
chroma run --host 0.0.0.0 --port 8000
```

### 5. (Optional) Configure MCP Servers

Create `~/.hermes/mcp.json`:

```json
{
  "servers": [
    {
      "name": "filesystem",
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/dir"]
    },
    {
      "name": "remote-api",
      "transport": "http",
      "url": "http://localhost:3000/mcp"
    }
  ]
}
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `OPENAI_API_KEY` | -- | **Required.** OpenAI-compatible API key |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Custom API base URL |
| `HERMES_MODEL` | `openai/gpt-4o` | Model identifier (`provider/model` format) |
| `HERMES_HOME` | `~/.hermes` | Root directory for config, memories, palace data |
| `HERMES_MEMPALACE` | `1` (enabled) | Set to `0` to disable the MemPalace provider |

---

## REPL Commands

| Command | Description |
|---|---|
| `/quit` or `/exit` | Gracefully close the agent, MCP servers, and exit |
| `/stats` | Show session statistics (iterations, tool calls, token usage) |
| `/budget` | Display remaining iteration budget |
| `/mcp` | List connected MCP servers and their registered tools |

Any other input is treated as a natural-language message sent to the agent.

---

## Project Structure

```
hermes_agent_go/
+-- cmd/hermes/
|   +-- main.go                 # Entry point and REPL
+-- pkg/
|   +-- agent/
|   |   +-- agent.go            # Core agent loop (think -> act -> observe)
|   |   +-- budget.go           # Iteration budget tracker
|   |   +-- parallel.go         # Parallel tool executor
|   |   +-- prompt/
|   |   |   +-- builder.go      # System prompt construction
|   |   +-- context/
|   |   |   +-- compressor.go   # Context window compression
|   |   +-- credential/
|   |   |   +-- pool.go         # API key pool management
|   |   +-- memory/
|   |       +-- provider.go     # Memory provider interface
|   |       +-- store.go        # Flat-file memory store
|   |       +-- manager.go      # Multi-provider memory manager
|   |       +-- builtin.go      # Built-in memory provider
|   |       +-- mempalace/
|   |           +-- doc.go          # Package documentation
|   |           +-- provider.go     # MemPalace provider (838 lines)
|   |           +-- layers.go       # 4-layer memory stack
|   |           +-- drawer.go       # Drawer store with callbacks
|   |           +-- searcher.go     # Hybrid BM25 + ChromaDB search
|   |           +-- knowledge_graph.go  # Temporal knowledge graph
|   +-- model/
|   |   +-- provider.go         # Model provider interface
|   |   +-- router.go           # Multi-provider model router
|   |   +-- openai/
|   |       +-- provider.go     # OpenAI-compatible provider
|   +-- tool/
|   |   +-- registry/
|   |   |   +-- registry.go     # Global tool registry
|   |   +-- builtin/            # Built-in tools
|   |   |   +-- bash.go         # Shell command execution
|   |   |   +-- read_file.go    # File reading
|   |   |   +-- write_file.go   # File writing
|   |   |   +-- edit_file.go    # File editing
|   |   |   +-- list_dir.go     # Directory listing
|   |   |   +-- search_files.go # File search (grep)
|   |   |   +-- skills.go       # Skill management
|   |   +-- delegate/
|   |   |   +-- delegate.go     # Sub-agent delegation
|   |   +-- mcp/
|   |       +-- manager.go      # MCP server lifecycle manager
|   |       +-- server.go       # MCP server abstraction
|   |       +-- stdio.go        # stdio transport
|   |       +-- http.go         # HTTP/SSE transport
|   |       +-- config.go       # MCP config loader
|   |       +-- types.go        # MCP protocol types
|   +-- types/
|   |   +-- config.go           # AgentConfig with defaults
|   |   +-- message.go          # Message types
|   |   +-- tool.go             # Tool definition types
|   +-- state/
|   |   +-- session_db.go       # Session persistence
|   +-- log/
|   |   +-- log.go              # Structured logging
|   +-- errx/
|       +-- errors.go           # Error types
+-- go.mod
+-- go.sum
+-- .gitignore
```

---

## Technical Principles

### Agent Loop

Hermes uses a **ReAct (Reasoning + Acting)** agent loop:

```
+---------+    +----------+    +---------+    +-------------+
|  Think  |--->| Act      |--->| Observe |--->| Continue?   |
|  (LLM)  |    | (Tools)  |    | (Result)|    | (Budget OK) |
+---------+    +----------+    +---------+    +------+------+
     ^                                               |
     +------------------- yes -----------------------+
```

1. **Think**: The LLM receives the conversation history + system prompt (including memory) and decides what to do next.
2. **Act**: If the LLM requests tool calls, they are executed -- up to **8 in parallel** when independent.
3. **Observe**: Tool results are appended to the conversation.
4. **Budget Check**: The loop continues until the LLM produces a final text response, or the iteration budget (default **90**) is exhausted.

Sub-agent delegation is supported up to **2 levels deep**, with delegated agents limited to **50 iterations** each.

### MemPalace 4-Layer Memory

MemPalace uses a **palace metaphor** to organize long-term memory into four layers with increasing retrieval cost:

```
Layer    Name              Token Budget   Loading Strategy
-----    ----              ------------   ----------------
 L0      Identity          ~100 tokens    Always loaded (system prompt)
 L1      Essential Story   ~800 tokens    Always loaded (auto-generated)
 L2      On-Demand         variable       Loaded per topic/wing
 L3      Deep Search       variable       On explicit palace_search call
```

**L0 -- Identity** (`identity.txt`): A concise self-description always injected into the system prompt. Defines the agent persona and behavioral guidelines.

**L1 -- Essential Story**: Auto-generated from the top-15 highest-importance drawers. Grouped by "room" (category), provides the agent with its most critical memories without any search cost.

**L2 -- On-Demand**: Wing-specific memories loaded when the conversation topic is detected. For example, entering a "work" conversation loads the work wing top drawers.

**L3 -- Deep Search**: Full hybrid search triggered by `palace_search` tool calls. Combines BM25 keyword matching with ChromaDB vector similarity for maximum recall.

#### Data Model: Wings, Rooms and Drawers

Memory is organized using a physical palace metaphor:

- **Wing**: Top-level category (e.g., `personal`, `work`, `projects`)
- **Room**: Sub-category within a wing (e.g., `work/team`, `projects/hermes`)
- **Drawer**: An individual memory unit with content, importance score (0-10), source attribution, and timestamp

### Hybrid Search Engine

The searcher combines two complementary retrieval strategies:

```
                    Query
                      |
          +-----------+-----------+
          v                       v
    +----------+           +----------+
    | ChromaDB |           |  BM25    |
    | (Vector) |           |(Keyword) |
    +----+-----+           +----+-----+
         |                      |
         v                      v
    vector_score           bm25_score
    (cosine sim)           (Okapi BM25)
         |                      |
         +----------+-----------+
                    v
         +-----------------+
         |  Hybrid Fusion  |
         |  0.6*vector +   |
         |  0.4*BM25_norm  |
         +--------+--------+
                  v
         +-----------------+
         |  Boost Signals  |
         |  + entity match |
         |  + importance   |
         +--------+--------+
                  v
           Ranked Results
```

**Scoring formula:**

```
final_score = 0.6 * vector_similarity + 0.4 * bm25_normalized
            + entity_boost + importance_weight
```

- **Vector similarity**: `max(0, 1 - cosine_distance)` from ChromaDB embeddings
- **BM25**: Classic Okapi BM25 with `k1=1.5, b=0.75`, normalized to `[0, 1]`
- **Entity boost**: +0.15 when query entities match drawer metadata
- **Importance weight**: `importance / 10 * 0.1` (high-importance memories float up)

> When ChromaDB is unavailable, the searcher transparently falls back to pure BM25 -- no configuration needed.

### Knowledge Graph

The temporal Knowledge Graph stores structured facts as **entity-relationship triples**:

```
Triple:  (Subject) --[Predicate]--> (Object)
         "Alice"   --[works_at]---> "Acme Corp"
         valid_from: 2024-01-15
         valid_to:   (empty = still valid)
         confidence: 0.95
```

Key features:

- **Temporal validity**: Each triple has `valid_from` / `valid_to` dates, allowing the agent to track fact changes over time (e.g., job changes, project transitions)
- **Entity typing**: Entities are typed (`person`, `project`, `tool`, `concept`) with arbitrary property maps
- **Multi-index lookups**: Indexed by subject, object, and predicate for fast graph traversal
- **Invalidation**: When facts change, old triples are marked with `valid_to` rather than deleted, preserving history
- **Persistence**: JSON-backed storage under `{palace}/kg/` (entities.json + triples.json)

The KG complements the Drawer-based text search by providing precise, structured fact retrieval -- ideal for questions like "Where does Alice work?" or "What tools does Project X use?"

### MCP Integration

Hermes supports the [Model Context Protocol](https://modelcontextprotocol.io/) for dynamic tool discovery:

- **Stdio transport**: Launch MCP servers as child processes communicating over stdin/stdout
- **HTTP transport**: Connect to remote MCP servers via HTTP/SSE
- **Auto-discovery**: On startup, connects to configured servers and registers their tools into the global registry
- **Lifecycle management**: Graceful closing of all MCP server connections on exit

Configuration lives in `~/.hermes/mcp.json`. All discovered MCP tools are seamlessly available alongside built-in tools.

### Tool System

Tools follow a unified interface and are registered in a global registry:

| Category | Tools | Description |
|---|---|---|
| **Builtin** | `bash`, `read_file`, `write_file`, `edit_file`, `list_dir`, `search_files` | Core file system and shell operations |
| **Delegate** | `delegate` | Spawn sub-agents for complex sub-tasks |
| **MCP** | (dynamic) | Tools discovered from MCP servers at runtime |
| **Memory** | `palace_search`, `palace_add`, `palace_kg_query`, `palace_kg_add`, `palace_kg_invalidate` | MemPalace memory interaction tools |

The parallel executor runs up to **8 independent tool calls concurrently**, significantly reducing latency for multi-tool reasoning steps.

---

## Configuration

Default agent configuration (from `pkg/types/config.go`):

```go
AgentConfig{
    Model:                 "openai/gpt-4o",
    Temperature:           0.7,
    MaxTokens:             16384,
    MaxIterations:         90,
    MaxParallelTools:      8,
    DelegateMaxIterations: 50,
    MaxDelegateDepth:      2,
    Platform:              "cli",
}
```

All values can be overridden via environment variables or programmatic configuration.

---

## Dependencies

Core dependency: [`chroma-go v0.4.1`](https://github.com/amikos-tech/chroma-go) for ChromaDB vector database integration.

Full dependency list available in `go.mod`.

---

## License

MIT
