# Hermes Agent Go

A production-grade AI agent framework written in Go, featuring JSON-defined graph-based orchestration, multi-layer memory architecture (MemPalace), hybrid vector + BM25 search, MCP (Model Context Protocol) integration, and human-in-the-loop support.

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
|  +-------------------------------------------------+ |
|  |          Graph Executor (pkg/orchestrator)       | |
|  |  LLM -> Choice -> Parallel(Tools) -> LLM -> End | |
|  |  (JSON-defined DAG, 6 node types, retry/catch)  | |
|  +-------------------------------------------------+ |
|         |                     |                       |
|  +------v------+  +-----------v------------------+   |
|  | LLMInvoker  |  | ToolInvoker                  |   |
|  | (model      |  | (tool.Registry +              |   |
|  |  .Router)   |  |  memory.Manager               |   |
|  +-------------+  |  + ask_human (HITL))          |   |
|                   +-------------------------------+   |
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

# Optional: model override in provider/model format (default: openai/gpt-4o).
# The provider prefix selects the backend: "deepseek/..." uses the DeepSeek
# provider; anything else uses the OpenAI-compatible provider.
export HERMES_MODEL="openai/gpt-4o"
# e.g. DeepSeek: export HERMES_MODEL="deepseek/deepseek-chat"
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
| `HERMES_MODEL` | `openai/gpt-4o` | Model in `provider/model` format. Prefix `deepseek/` selects the DeepSeek provider; otherwise the OpenAI-compatible provider is used |
| `HERMES_HOME` | `~/.hermes` | Root directory for config, memories, palace data |
| `HERMES_MEMPALACE` | `1` (enabled) | Set to `0` to disable the MemPalace provider |

---

## REPL Commands

| Command | Description |
|---|---|
| `/quit` or `/exit` | Gracefully close the agent, persist messages, and exit |
| `/stats` | Show session statistics (iterations, tool calls, token usage) |
| `/budget` | Display remaining iteration budget |
| `/todo` | Show current TODO/planning list |
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
|   |   +-- agent.go            # Core agent (graph executor based)
|   |   +-- graph_builder.go    # Default agent graph definition
|   |   +-- event_tracer.go     # Tracer -> EventCallback bridge
|   |   +-- adapters/
|   |   |   +-- llm_invoker.go  # model.Router -> LLMInvoker
|   |   |   +-- tool_invoker.go # tool.Registry -> ToolInvoker
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
|   |           +-- provider.go     # MemPalace provider
|   |           +-- layers.go       # 4-layer memory stack
|   |           +-- drawer.go       # Drawer store with callbacks
|   |           +-- searcher.go     # Hybrid BM25 + ChromaDB search
|   |           +-- knowledge_graph.go  # Temporal knowledge graph
|   +-- orchestrator/           # Graph-based orchestration engine
|   |   +-- node.go             # NodeRunner interface + NodeResult
|   |   +-- graph.go            # Graph, NodeSpec, EdgeSpec types
|   |   +-- graph_json.go       # JSON two-phase graph loading
|   |   +-- registry.go         # Thread-safe type registry
|   |   +-- tracer.go           # Tracer interface + NopTracer
|   |   +-- executor/
|   |   |   +-- executor.go     # Graph walker (retry/catch/interrupt)
|   |   |   +-- route.go        # Edge routing with priority
|   |   |   +-- stream.go       # ExecuteStream wrapper
|   |   +-- runner/
|   |   |   +-- llm.go          # LLMRunner + LLMInvoker
|   |   |   +-- tool.go         # ToolRunner + ToolInvoker
|   |   |   +-- choice.go       # ChoiceRunner (condition branch)
|   |   |   +-- parallel.go     # ParallelRunner (concurrent)
|   |   |   +-- human.go        # HumanRunner (HITL interrupt)
|   |   |   +-- end.go          # EndRunner (terminal)
|   |   +-- context/
|   |   |   +-- execution.go    # ExecutionContext / WorkingMemory
|   |   |   +-- memory.go       # ConversationMemory / MemoryStore
|   |   +-- schema/
|   |       +-- pipe.go         # Pipe / StreamReader / StreamWriter
|   +-- model/
|   |   +-- provider.go         # Model provider interface
|   |   +-- router.go           # Multi-provider model router
|   |   +-- openai/
|   |   |   +-- provider.go     # OpenAI-compatible provider
|   |   +-- deepseek/
|   |       +-- provider.go     # DeepSeek provider (reasoning_content, cache tokens)
|   |       +-- retry.go        # Retry / backoff config
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

Hermes uses a **JSON-defined Graph** for agent orchestration. The default graph implements a ReAct-style loop:

```
┌──────┐    ┌────────┐    ┌──────────────────┐
│ LLM  │───→│ Choice │───→│ Parallel(Tool...) │──┐
└──────┘    └───┬────┘    └──────────────────┘  │
                │ no tool_calls                  │
                ▼                                │
            ┌──────┐                             │
            │ End  │                             │
            └──────┘    ◄────────────────────────┘
```

The graph engine (`pkg/orchestrator/`) supports 6 node types:

| Node | Description |
|---|---|
| `llm` | Invokes the LLM via `LLMInvoker` interface, streams deltas |
| `choice` | Routes to next node based on JSON conditions (e.g. `has_tool_calls`) |
| `tool` | Executes a tool via `ToolInvoker`, including `ask_human` for HITL |
| `parallel` | Runs multiple sub-graph branches concurrently |
| `human` | Pauses execution and waits for human input (returns `Interrupt`) |
| `end` | Terminates the graph, returns output |

Each node supports **retry** (exponential backoff with jitter) and **catch** (error routing to fallback nodes). The graph is defined as JSON and can be overridden by users via config.

Sub-agent delegation is supported up to **2 levels deep**, with delegated agents limited to **50 iterations** each.

#### Human-in-the-Loop

When the LLM calls `ask_human(question, options)`, the executor pauses and returns an `ExecutionSnapshot`. The caller presents the question to the user, collects the response, and calls `executor.Resume(snapshot, response)`. Snapshots are persisted to SessionDB for crash-safe recovery.

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
| **Skills** | `skills` | Discover, activate, and load SKILL.md-based capabilities on demand (per-agent activation state) |
| **Web** | `web_get`, `web_post`, `web_scrape`, `web_download` | HTTP requests, web scraping, and file downloads |
| **Terminal** | `terminal_exec`, `terminal_interactive`, `terminal_info`, `terminal_parse`, `terminal_script` | Terminal command execution and interaction |
| **Delegate** | `delegate_task` | Spawn sub-agents for complex sub-tasks |
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

Configuration is loaded from (in priority order: env > config file > defaults):
1. `hermes.yaml` or `hermes.json` in the current directory
2. `~/.hermes/config.yaml` or `~/.hermes/config.json`

YAML example with custom provider:

```yaml
model: "doubao/ep-20260415120037-dtjmn"
custom_providers:
- name: doubao
  base_url: https://your-api.example.com/v1
  api_key: your-api-key
  model: ep-20260415120037-dtjmn
  models:
    ep-20260415120037-dtjmn:
      context_length: 300000
```

---

## Dependencies

Core dependencies:
- [`chroma-go v0.4.1`](https://github.com/amikos-tech/chroma-go) for ChromaDB vector database integration
- [`goquery v1.12.0`](https://github.com/PuerkitoBio/goquery) for web scraping and HTML parsing

Full dependency list available in `go.mod`.

---

## License

MIT
