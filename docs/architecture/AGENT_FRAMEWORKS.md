# Agent Framework Adapters Architecture

> **Status:** Authoritative Design Document
> **Audience:** Engineers implementing or extending the OAP agent-adapter subsystem
> **Date:** 2026-06-15
> **Source Plan:** `docs/plans/MASTER_IMPLEMENTATION_PLAN.md` §3.3

---

## Table of Contents

1. [Overview](#1-overview)
2. [AgentWrapper Interface](#2-agentwrapper-interface)
3. [Framework Adapters](#3-framework-adapters)
   - 3.1 [LangGraph](#31-langgraph-adapter)
   - 3.2 [CrewAI](#32-crewai-adapter)
   - 3.3 [AutoGen](#33-autogen-adapter)
   - 3.4 [Semantic Kernel](#34-semantic-kernel-adapter)
   - 3.5 [OpenAI Agents SDK](#35-openai-agents-sdk-adapter)
   - 3.6 [Anthropic Claude](#36-anthropic-claude-adapter)
4. [ProcessPool](#4-processpool)
5. [OrchestrationService](#5-orchestrationservice)
6. [Cost Management](#6-cost-management)
7. [Human-in-the-Loop](#7-human-in-the-loop)
8. [Implementation Steps](#8-implementation-steps)

---

## 1. Overview

### 1.1 Why Adapters

OpenAgentPlatform supports six independent LLM agent frameworks — LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, and Anthropic Claude Agent SDK. Each framework exposes a different invocation API, state model, streaming primitive, and tool-calling convention. Without a unifying layer, the A2A gateway, the dashboard, and the orchestration engine would each need to know about all six APIs.

The **adapter pattern** solves this by defining a single abstract base class — `AgentWrapper` — that every framework adapter implements. The rest of the platform interacts exclusively with this interface.

```
┌────────────────────────────────────────────────────┐
│                  A2A Gateway                        │
│  JSON-RPC / SSE / REST  ·  AgentCard discovery      │
│  Task lifecycle · Skill-based routing               │
└──────────────────────┬─────────────────────────────┘
                       │  invokes AgentWrapper
       ┌───────────────┼───────────────┐
       │               │               │
  ┌────┴────┐    ┌─────┴────┐    ┌─────┴────┐
  │ LangGr. │    │  CrewAI  │    │  AutoGen │  ... (6 adapters)
  └─────────┘    └──────────┘    └─────────┘
       │               │               │
  ┌────┴───────────────┴───────────────┴────┐
  │           ProcessPool                     │
  │  Warm subprocesses · LRU · Health check   │
  └───────────────────────────────────────────┘
```

### 1.2 What `AgentWrapper` Is

`AgentWrapper` is an abstract base class (ABC) that:

- Normalizes the **six** different framework invocation styles into a single async interface.
- Translates **A2A protocol messages** (text, file, data Parts) into framework-native inputs and back.
- Exposes an **AgentCard** for discovery and a list of **AgentSkill** entries for capability-based routing.
- Supports both **request/response** (`invoke`) and **streaming** (`stream`) semantics.
- Allows **cancellation** of in-flight tasks.

Every adapter in the subsystem inherits from this ABC and provides concrete implementations of its five methods. The `OrchestrationService` and `ProcessPool` are the only components that hold references to concrete adapter instances; all other code works through the interface.

### 1.3 How A2A Connects to LLM Frameworks

The A2A protocol defines a standard inter-agent communication contract: `SendMessage`, `SendStreamingMessage`, `CancelTask`, `GetTask`, and `SubscribeToTask`. Each method carries an `A2AMessage` (with `Parts`) and returns an `A2ATaskResult` (with `Artifacts`) or a stream of `A2ADeltaEvent` objects.

The adapter layer is the **bridge** between this protocol and the frameworks:

| A2A Concept | Adapter Responsibility | LangGraph Example | CrewAI Example |
|-------------|----------------------|-------------------|----------------|
| `SendMessage` | Translate Parts → framework input | `HumanMessage` in state dict | `kickoff_async(inputs={...})` |
| `SendStreamingMessage` | Wrap framework streaming → deltas | `astream()` tokens | `kickoff_async(..., stream=True)` |
| `CancelTask` | Signal framework to abort | `thread.interrupt()` | `crew.cancel()` |
| `AgentCard` | Expose static metadata | Graph name, version, skills | Crew name, role, tools |
| `AgentSkill` | Expose discoverable capabilities | Node names, tool names | Agent roles, tool names |
| `Artifact` | Extract structured output from state | `state["output"]` key | `crew.output` |

---

## 2. AgentWrapper Interface

### 2.1 ABC Definition

```python
from abc import ABC, abstractmethod
from typing import AsyncIterator

class AgentWrapper(ABC):
    """
    Abstract base class for all framework adapters.

    Every concrete adapter MUST implement all five abstract methods.
    The OrchestrationService and ProcessPool interact exclusively
    through this interface.
    """

    @abstractmethod
    async def invoke(
        self,
        message: A2AMessage,
        config: dict,
    ) -> A2ATaskResult:
        """
        Execute a single request/response interaction with the agent.

        Args:
            message:  An A2A protocol message containing Parts (text, file, data).
            config:   Framework-specific configuration (model name, temperature,
                      tool allow-list, timeout, credentials reference).

        Returns:
            A2ATaskResult with terminal state (COMPLETED / FAILED / INPUT_REQUIRED)
            and zero or more Artifacts.
        """
        ...

    @abstractmethod
    async def stream(
        self,
        message: A2AMessage,
        config: dict,
    ) -> AsyncIterator[A2ADeltaEvent]:
        """
        Execute a streaming interaction, yielding delta events as the agent
        produces tokens, tool calls, or intermediate artifacts.

        Args:
            message:  An A2A protocol message.
            config:   Framework-specific configuration.

        Yields:
            A2ADeltaEvent objects — each carries a delta payload (text chunk,
            tool-call record, artifact fragment, or state update).
        """
        ...

    @abstractmethod
    async def cancel(self, task_id: str) -> None:
        """
        Cancel an in-flight task.

        Args:
            task_id:  The A2A task identifier returned by a prior invoke/stream call.

        Raises:
            TaskNotFoundError:  If no task with that ID exists.
            TaskAlreadyTerminal: If the task is already COMPLETED or FAILED.
        """
        ...

    @abstractmethod
    def card(self) -> AgentCard:
        """
        Return the static AgentCard for this adapter.

        The AgentCard contains:
            - name, version, description
            - provider organization
            - URL endpoint (if hosted remotely)
            - capabilities (streaming, pushNotifications, stateTransitionHistory)
            - default input/output modes (text, file, data)
            - skills (list of AgentSkill)

        Returns:
            An immutable AgentCard instance.
        """
        ...

    @abstractmethod
    def skills(self) -> list[AgentSkill]:
        """
        Return the list of skills this agent exposes.

        Skills are the unit of discovery and routing. A2A clients query
        the AgentCard's skills to determine which agent can handle a
        given request.

        Returns:
            A list of AgentSkill objects, each with id, name, description,
            tags, input modes, output modes, and optional examples.
        """
        ...
```

### 2.2 Method Contract Summary

| Method | Async | Returns | Purpose |
|--------|-------|---------|---------|
| `invoke` | Yes | `A2ATaskResult` | Full request/response; terminal state + artifacts |
| `stream` | Yes | `AsyncIterator[A2ADeltaEvent]` | Incremental output; yields until terminal state |
| `cancel` | Yes | `None` | Abort an in-flight task by ID |
| `card` | No | `AgentCard` | Static discovery metadata |
| `skills` | No | `list[AgentSkill]` | Capability descriptors for routing |

### 2.3 Key Design Properties

- **Stateless by contract.** `invoke` and `stream` receive all context in the `message` and `config` parameters. The adapter does not maintain conversation history between calls (state is passed in via `config["thread_id"]` or equivalent).
- **Cancellation is cooperative.** The adapter translates the A2A cancel signal into a framework-native abort (e.g., `thread.cancel()`, `crew.cancel()`, `asyncio.CancelledError`). Hard-kill fallback is handled by the ProcessPool.
- **AgentCard is static.** `card()` returns a pre-built immutable object. Adapters do not perform I/O when answering discovery queries.
- **Skills are dynamic-safe.** `skills()` can return a list computed at startup. It should not require a live connection to the LLM provider.

---

## 3. Framework Adapters

### 3.1 LangGraph Adapter

**Package path:** `adapters/src/oap/adapters/langgraph/`
**Purpose:** Wrap a compiled LangGraph `StateGraph` so it can receive A2A messages and stream A2A deltas.

#### 3.1.1 State Dict Translation

LangGraph operates on a shared state dict (typically `MessagesState` with a `messages` key). The adapter converts an incoming `A2AMessage` into a state update:

```python
def _message_to_state(self, message: A2AMessage) -> dict:
    """Convert A2AMessage parts into a LangGraph state update."""
    parts = []
    for part in message.parts:
        if part.type == "text":
            parts.append(HumanMessage(content=part.text))
        elif part.type == "data":
            parts.append(HumanMessage(content=json.dumps(part.data)))
        elif part.type == "file":
            # File parts are passed as references; the LLM may request download
            parts.append(HumanMessage(content=f"[file: {part.filename}]"))
    return {"messages": parts}
```

The `config` dict is translated into a LangGraph `RunnableConfig`:

```python
def _config_to_runnable(self, config: dict) -> dict:
    return {
        "configurable": {
            "thread_id": config.get("thread_id", str(uuid4())),
            "checkpoint_ns": config.get("checkpoint_ns", ""),
        },
        "recursion_limit": config.get("recursion_limit", 25),
    }
```

#### 3.1.2 Compiled Graph Execution

The adapter holds a reference to a compiled graph (built once at startup) and invokes it:

```python
async def invoke(self, message: A2AMessage, config: dict) -> A2ATaskResult:
    state_update = self._message_to_state(message)
    runnable_config = self._config_to_runnable(config)
    result = await self.graph.ainvoke(state_update, config=runnable_config)
    return self._extract_artifacts(result, runnable_config["configurable"]["thread_id"])
```

For streaming:

```python
async def stream(self, message: A2AMessage, config: dict):
    state_update = self._message_to_state(message)
    runnable_config = self._config_to_runnable(config)
    async for event in self.graph.astream(state_update, config=runnable_config, stream_mode="values"):
        yield A2ADeltaEvent(
            type="state_update",
            payload=event,
            task_id=runnable_config["configurable"]["thread_id"],
        )
```

#### 3.1.3 Artifact Extraction

LangGraph state may contain structured outputs in designated keys. The adapter inspects the final state for known artifact keys:

```python
ARTIFACT_KEYS = ["output", "result", "report", "artifacts"]

def _extract_artifacts(self, final_state: dict, task_id: str) -> A2ATaskResult:
    artifacts = []
    for key in ARTIFACT_KEYS:
        if key in final_state:
            artifacts.append(Artifact(
                artifact_id=f"{task_id}-{key}",
                parts=[DataPart(value=final_state[key])],
                name=key,
            ))
    return A2ATaskResult(
        task_id=task_id,
        state=TaskState.COMPLETED,
        artifacts=artifacts,
    )
```

If the graph invokes `interrupt()` (LangGraph's built-in human-in-the-loop mechanism), the adapter maps this to `TaskState.INPUT_REQUIRED` and surfaces the interrupt payload as an artifact.

#### 3.1.4 LangSmith A2A Endpoint

LangGraph graphs can be deployed to **LangSmith Agent Server**, which natively exposes an A2A endpoint. When the adapter is configured with a `remote_url`, it becomes a thin HTTP client:

```python
class LangGraphAdapter(AgentWrapper):
    def __init__(self, compiled_graph=None, remote_url: str | None = None):
        self.graph = compiled_graph
        self.remote_url = remote_url  # LangSmith Agent Server URL

    async def invoke(self, message, config):
        if self.remote_url:
            return await self._invoke_remote(message, config)
        return await self._invoke_local(message, config)
```

The remote mode POSTs to `{remote_url}/a2a` and decodes the JSON-RPC response, making the adapter a pass-through when LangSmith hosting is preferred.

#### 3.1.5 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    # LangGraph supports cancellation via thread.interrupt() in a follow-up call
    # The adapter uses the checkpointer to signal the thread
    config = {"configurable": {"thread_id": task_id}}
    await self.graph.aupdate_state(config, {"__cancel__": True})
```

#### 3.1.6 AgentCard and Skills

The `card()` method returns metadata derived from the graph's name, version, and node list. The `skills()` method exposes each named node as an AgentSkill, with the node's docstring as the description.

---

### 3.2 CrewAI Adapter

**Package path:** `adapters/src/oap/adapters/crewai/`
**Purpose:** Wrap a CrewAI `Crew` (a team of agents) so it can be invoked via A2A, with native A2A peer registration.

#### 3.2.1 Leveraging the Native `crewai.a2a` Module

CrewAI ships with a first-party `crewai.a2a` module that provides native A2A protocol support. The adapter delegates to it directly rather than reimplementing protocol handling:

```python
from crewai.a2a import A2AServer, A2AClient

class CrewAIAdapter(AgentWrapper):
    def __init__(self, crew: Crew, peer_registrations: list[AgentCard] | None = None):
        self.crew = crew
        self.a2a_server = A2AServer(crew=crew)
        self.a2a_client = A2AClient(peers=peer_registrations or [])
```

The `crewai.a2a` module handles JSON-RPC serialization, task ID generation, and SSE streaming. The adapter focuses on translating A2A concepts into CrewAI inputs and extracting results from crew outputs.

#### 3.2.2 A2A Message to Crew Inputs

```python
def _message_to_inputs(self, message: A2AMessage) -> dict:
    """Convert A2A message parts into crew kickoff inputs."""
    inputs = {}
    for part in message.parts:
        if part.type == "text":
            inputs["topic"] = part.text
        elif part.type == "data":
            inputs.update(part.data)
    return inputs
```

#### 3.2.3 Invocation

```python
async def invoke(self, message: A2AMessage, config: dict) -> A2ATaskResult:
    inputs = self._message_to_inputs(message)
    result = await self.crew.kickoff_async(inputs=inputs)
    return A2ATaskResult(
        task_id=result.task_id,
        state=TaskState.COMPLETED,
        artifacts=[Artifact(
            artifact_id=f"{result.task_id}-output",
            parts=[TextPart(value=str(result.raw))],
        )],
    )
```

For streaming, CrewAI supports step-by-step output via `kickoff_async(stream=True)`:

```python
async def stream(self, message, config):
    inputs = self._message_to_inputs(message)
    async for step in self.crew.kickoff_async(inputs=inputs, stream=True):
        yield A2ADeltaEvent(
            type="step",
            payload={"agent": step.agent, "output": step.output},
        )
```

#### 3.2.4 Peer Registration

The adapter registers the crew's A2A endpoint with peer agents (other crews or external agents) during initialization. This allows the crew to **discover** and **delegate** to other A2A-compliant agents:

```python
# At startup:
for peer_card in peer_registrations:
    await self.a2a_client.register_peer(peer_card)

# During execution, an agent in the crew can send a message to a peer:
result = await self.a2a_client.send_message(peer_card, message)
```

Peer discovery uses the A2A AgentCard mechanism: each peer publishes its card at a well-known URL, and the adapter's `a2a_client` fetches and caches these cards.

#### 3.2.5 Within-Crew vs Cross-Crew Communication

| Mode | Description | A2A Mapping |
|------|-------------|-------------|
| **Within-crew** | Agents in the same crew communicate via CrewAI's internal delegation (no A2A). | Not visible to A2A layer; the adapter sees only the crew output. |
| **Cross-crew** | One crew delegates a subtask to another crew or external agent. | Each delegation is a separate A2A `SendMessage` call. The adapter's `a2a_client` handles the call. |

The adapter records which mode was used in the task metadata, enabling observability and cost attribution.

#### 3.2.6 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    # CrewAI supports cancellation by task_id
    self.crew.cancel(task_id=task_id)
```

#### 3.2.7 AgentCard and Skills

Each agent in the crew contributes one or more skills to the crew's AgentCard. The card's skills list is the union of all agent tools and capabilities.

---

### 3.3 AutoGen Adapter

**Package path:** `adapters/src/oap/adapters/autogen/`
**Purpose:** Wrap an AutoGen `GroupChat` (or `RoutedAgent` group) so multi-agent conversations map to A2A tasks.

#### 3.3.1 GroupChat to SendMessage Mapping

AutoGen's `GroupChat` allows multiple agents to converse under a `GroupChatManager`. Each message in the group chat is a turn by one agent. The adapter maps this to A2A as follows:

| AutoGen Concept | A2A Mapping |
|-----------------|-------------|
| `GroupChat` | One A2A task (with a task_id) |
| Agent turn (message) | One `A2ADeltaEvent` of type `"agent_message"` |
| `GroupChatManager.select_speaker()` | Internal; not surfaced to A2A |
| `GroupChat.run()` completion | Terminal `A2ATaskResult` |
| `GroupChat` cancellation | `cancel(task_id)` |

```python
class AutoGenAdapter(AgentWrapper):
    def __init__(self, group_chat: GroupChat, manager: GroupChatManager):
        self.chat = group_chat
        self.manager = manager
        self._active_tasks: dict[str, asyncio.Event] = {}

    async def stream(self, message: A2AMessage, config: dict):
        task_id = str(uuid4())
        cancel_event = asyncio.Event()
        self._active_tasks[task_id] = cancel_event

        user_text = self._extract_text(message)
        # Inject the A2A message as the initial user turn
        chat_result = await self.manager.a_initiate_chat(
            self.chat,
            message=user_text,
            cancel_event=cancel_event,
        )

        async for msg in self._stream_chat_messages(chat_result):
            yield A2ADeltaEvent(type="agent_message", payload=msg, task_id=task_id)

        yield A2ADeltaEvent(
            type="terminal",
            payload={"state": TaskState.COMPLETED},
            task_id=task_id,
        )
```

#### 3.3.2 Termination Conditions to Task States

AutoGen v0.4+ supports `TerminationCondition` objects (e.g., `MaxMessageTermination`, `TokenUsageTermination`, `HandoffTermination`). The adapter maps these to A2A `TaskState` values:

| Termination Condition | A2A TaskState |
|----------------------|---------------|
| `MaxMessageTermination` reached | `COMPLETED` |
| `TextMentionTermination` with "TERMINATE" | `COMPLETED` |
| `TokenUsageTermination` exceeded budget | `FAILED` (with `reason="budget_exceeded"`) |
| `HandoffTermination` (agent hands off to non-existent target) | `FAILED` |
| `TimeoutTermination` | `CANCELED` |
| `SourceMatchTermination` (expected source responded) | `COMPLETED` |
| Cancellation event set | `CANCELED` |

```python
def _map_termination(self, condition) -> TaskState:
    if isinstance(condition, (MaxMessageTermination, TextMentionTermination, SourceMatchTermination)):
        return TaskState.COMPLETED
    if isinstance(condition, TokenUsageTermination):
        return TaskState.FAILED
    if isinstance(condition, TimeoutTermination):
        return TaskState.CANCELED
    return TaskState.FAILED
```

#### 3.3.3 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    if task_id in self._active_tasks:
        self._active_tasks[task_id].set()  # Signal the cancel event
        del self._active_tasks[task_id]
```

#### 3.3.4 AgentCard and Skills

The card lists each agent in the group chat by name and role. Skills correspond to the tools and functions registered on each agent.

---

### 3.4 Semantic Kernel Adapter

**Package path:** `adapters/src/oap/adapters/semantic_kernel/`
**Purpose:** Wrap a Semantic Kernel `HandoffOrchestration` (or `ChatCompletionAgent`) so it can communicate via A2A, leveraging the Microsoft Agent Framework (MAF) for A2A hosting.

#### 3.4.1 HandoffOrchestration Wrapping

Semantic Kernel's `HandoffOrchestration` allows a primary agent to hand off control to specialized agents. The adapter wraps this orchestration:

```python
from semantic_kernel.agents import HandoffOrchestration, ChatCompletionAgent
from semantic_kernel.agents.runtime import InProcessRuntime

class SemanticKernelAdapter(AgentWrapper):
    def __init__(self, orchestration: HandoffOrchestration):
        self.orchestration = orchestration
        self.runtime = InProcessRuntime()
        self.runtime.start()
```

#### 3.4.2 ChatHistoryAgentThread

Semantic Kernel agents maintain conversation history in `ChatHistoryAgentThread` objects. The adapter creates a thread per A2A task, keyed by `thread_id`:

```python
def _get_or_create_thread(self, config: dict) -> ChatHistoryAgentThread:
    thread_id = config.get("thread_id")
    if thread_id and thread_id in self._threads:
        return self._threads[thread_id]
    thread = ChatHistoryAgentThread()
    self._threads[thread_id] = thread
    return thread
```

#### 3.4.3 Invocation

```python
async def invoke(self, message: A2AMessage, config: dict) -> A2ATaskResult:
    thread = self._get_or_create_thread(config)
    user_text = self._extract_text(message)

    responses = []
    async for response in self.orchestration.invoke_stream(
        thread=thread,
        message=user_text,
    ):
        responses.append(response)

    return A2ATaskResult(
        task_id=config.get("thread_id", str(uuid4())),
        state=TaskState.COMPLETED,
        artifacts=[Artifact(
            parts=[TextPart(value=str(responses[-1].content))],
        )],
    )
```

#### 3.4.4 MAF A2A Hosting

The **Microsoft Agent Framework (MAF)** — the successor to Semantic Kernel's agent runtime — provides native A2A protocol hosting. When the adapter is configured with `maf_endpoint`, it registers the orchestration as an A2A server:

```python
class SemanticKernelAdapter(AgentWrapper):
    def __init__(self, orchestration, maf_endpoint: str | None = None):
        self.orchestration = orchestration
        if maf_endpoint:
            from maf.a2a import A2AServer
            self.a2a_server = A2AServer(orchestration, bind=maf_endpoint)
```

This allows the orchestration to be discovered and invoked by any A2A-compliant client without the adapter acting as a translation layer.

#### 3.4.5 Handoff as A2A SendMessage

When a handoff occurs (agent A delegates to agent B), the adapter emits an `A2ADeltaEvent` of type `"handoff"`:

```python
async for response in self.orchestration.invoke_stream(thread=thread, message=user_text):
    if response.handoff_from and response.handoff_to:
        yield A2ADeltaEvent(
            type="handoff",
            payload={"from": response.handoff_from, "to": response.handoff_to},
        )
    yield A2ADeltaEvent(type="message", payload=response.content)
```

#### 3.4.6 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    thread = self._threads.get(task_id)
    if thread:
        await thread.cancel()
```

#### 3.4.7 AgentCard and Skills

The card lists the orchestration's primary agent and all handoff targets. Each target agent's tools and capabilities are exposed as skills.

---

### 3.5 OpenAI Agents SDK Adapter

**Package path:** `adapters/src/oap/adapters/openai/`
**Purpose:** Wrap an OpenAI Agents SDK agent (with handoffs and tools) so it can communicate via A2A.

#### 3.5.1 Handoff to SendMessage Delegation

The OpenAI Agents SDK supports agent-to-agent **handoffs** (one agent delegates control to another). The adapter maps each handoff to an A2A `SendMessage` event:

```python
from agents import Agent, Runner

class OpenAIAdapter(AgentWrapper):
    def __init__(self, agent: Agent):
        self.agent = agent
        self._handoff_log: list[dict] = []

    async def stream(self, message: A2AMessage, config: dict):
        user_text = self._extract_text(message)
        result = Runner.run_streamed(self.agent, input=user_text)

        async for event in result.stream_events():
            if event.type == "raw_response_event":
                yield A2ADeltaEvent(type="text_delta", payload=event.data)
            elif event.type == "run_item_stream_event":
                if event.item.type == "handoff_call_item":
                    self._handoff_log.append({
                        "from": self.agent.name,
                        "to": event.item.recipient,
                    })
                    yield A2ADeltaEvent(
                        type="handoff",
                        payload={"from": self.agent.name, "to": event.item.recipient},
                    )
```

#### 3.5.2 Tool Calls to MCP Invocations

The OpenAI Agents SDK supports `function_tool` definitions. The adapter bridges these to **MCP tool invocations** so that agents can call platform tools:

```python
from mcp import ClientSession

class OpenAIAdapter(AgentWrapper):
    def __init__(self, agent: Agent, mcp_session: ClientSession):
        self.agent = agent
        self.mcp = mcp_session
        # Register MCP tools as function tools on the agent
        for tool in await self.mcp.list_tools():
            self.agent.tools.append(self._wrap_mcp_tool(tool))

    def _wrap_mcp_tool(self, mcp_tool):
        async def tool_fn(**kwargs):
            result = await self.mcp.call_tool(mcp_tool.name, kwargs)
            return result
        tool_fn.__name__ = mcp_tool.name
        tool_fn.__doc__ = mcp_tool.description
        return tool_fn
```

When the LLM calls a tool, the adapter routes the call to the MCP server, receives the result, and feeds it back to the LLM — all invisible to the A2A client.

#### 3.5.3 Streaming to Artifacts

Tool calls and their results are emitted as `A2ADeltaEvent` objects. The final agent output is packaged as an artifact:

```python
async def stream(self, message, config):
    # ... streaming logic as above ...
    final_output = result.final_output
    yield A2ADeltaEvent(
        type="artifact",
        payload=Artifact(
            parts=[TextPart(value=final_output)],
        ),
    )
```

#### 3.5.4 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    # OpenAI Agents SDK supports cancellation via the Runner
    Runner.cancel(task_id=task_id)
```

#### 3.5.5 AgentCard and Skills

The card exposes the agent's name, model, and instruction. Skills correspond to the agent's tools (both native function tools and MCP-wrapped tools).

---

### 3.6 Anthropic Claude Adapter

**Package path:** `adapters/src/oap/adapters/anthropic/`
**Purpose:** Wrap an Anthropic Claude agent (using the Claude Agent SDK or direct tool-use API) so it can communicate via A2A.

#### 3.6.1 Tool-Use as A2A Skills

Claude's tool-use API allows the model to call named tools with structured inputs. The adapter treats each tool definition as an A2A skill:

```python
import anthropic

class AnthropicAdapter(AgentWrapper):
    def __init__(self, model: str, tools: list[dict], system: str = ""):
        self.client = anthropic.AsyncAnthropic()
        self.model = model
        self.tools = tools  # Claude-format tool definitions
        self.system = system
```

The adapter's `skills()` method converts each Claude tool definition into an `AgentSkill`:

```python
def skills(self) -> list[AgentSkill]:
    return [
        AgentSkill(
            id=tool["name"],
            name=tool["name"],
            description=tool.get("description", ""),
            tags=[tool.get("category", "general")],
            input_modes=["data"],
            output_modes=["data", "text"],
        )
        for tool in self.tools
    ]
```

#### 3.6.2 Streaming to Artifact Streams

Claude's Messages API supports streaming via `client.messages.stream()`. The adapter wraps this stream and converts each event to an `A2ADeltaEvent`:

```python
async def stream(self, message: A2AMessage, config: dict):
    user_text = self._extract_text(message)
    tool_calls = []
    text_blocks = []

    async with self.client.messages.stream(
        model=self.model,
        max_tokens=config.get("max_tokens", 4096),
        system=self.system,
        tools=self.tools,
        messages=[{"role": "user", "content": user_text}],
    ) as stream:
        async for event in stream:
            if event.type == "content_block_start":
                if event.content_block.type == "tool_use":
                    yield A2ADeltaEvent(
                        type="tool_call_start",
                        payload={"id": event.content_block.id, "name": event.content_block.name},
                    )
            elif event.type == "content_block_delta":
                if event.delta.type == "text_delta":
                    yield A2ADeltaEvent(type="text_delta", payload=event.delta.text)
                elif event.delta.type == "input_json_delta":
                    yield A2ADeltaEvent(
                        type="tool_call_delta",
                        payload={"id": event.content_block.id, "json": event.delta.partial_json},
                    )
            elif event.type == "content_block_stop":
                if event.content_block.type == "tool_use":
                    yield A2ADeltaEvent(
                        type="tool_call_complete",
                        payload={
                            "id": event.content_block.id,
                            "name": event.content_block.name,
                            "input": event.content_block.input,
                        },
                    )
            elif event.type == "message_stop":
                yield A2ADeltaEvent(type="terminal", payload={"state": TaskState.COMPLETED})
```

#### 3.6.3 Tool Execution

When Claude issues a tool call, the adapter executes the tool (either locally or via MCP) and feeds the result back:

```python
async def _execute_tool(self, tool_name: str, tool_input: dict) -> str:
    handler = self.tool_handlers.get(tool_name)
    if handler:
        result = await handler(**tool_input)
        return json.dumps(result)
    return json.dumps({"error": f"Unknown tool: {tool_name}"})
```

#### 3.6.4 Cancellation

```python
async def cancel(self, task_id: str) -> None:
    # Anthropic API does not support mid-stream cancellation,
    # but the adapter can abort the asyncio task wrapper
    task = self._active_streams.get(task_id)
    if task:
        task.cancel()
```

#### 3.6.5 AgentCard and Skills

The card lists the Claude model version, system prompt summary, and available tools (skills). Tags include the tool categories (e.g., `"file-system"`, `"network"`, `"database"`).

---

## 4. ProcessPool

### 4.1 Purpose

Agent framework subprocesses are expensive to initialize (loading models, establishing connections, building graphs). The **ProcessPool** maintains a **warm pool** of pre-initialized subprocesses so that `invoke` and `stream` calls have near-zero cold-start latency.

### 4.2 Class Definition

```python
class ProcessPool:
    def __init__(self, max_size: int = 10, idle_timeout: int = 300):
        """
        Args:
            max_size:      Maximum number of warm subprocesses to keep alive.
            idle_timeout:  Seconds a subprocess can be idle before LRU eviction.
        """
        self.max_size = max_size
        self.idle_timeout = idle_timeout
        self._pool: OrderedDict[str, SubprocessHandle] = OrderedDict()
        self._lock = asyncio.Lock()
```

### 4.3 Key Methods

#### 4.3.1 `acquire`

```python
async def acquire(self, adapter_type: str, config: dict) -> SubprocessHandle:
    """
    Acquire a subprocess handle for the given adapter type.

    If a warm handle is available, return it (LRU: move to end).
    If the pool is full, evict the least-recently-used idle handle.
    If no warm handle is available, spawn a new subprocess (up to max_size).

    Args:
        adapter_type:  The adapter class name (e.g., "LangGraphAdapter").
        config:        Configuration passed to the subprocess at spawn time.

    Returns:
        A SubprocessHandle with a stdin/stdout pipe or socket.
    """
```

#### 4.3.2 `release`

```python
async def release(self, handle: SubprocessHandle) -> None:
    """
    Return a handle to the pool after use.

    The handle is marked as idle and its last-used timestamp is updated.
    If the pool exceeds max_size after release, LRU eviction is triggered.
    """
```

#### 4.3.3 `health_check`

```python
async def health_check(self) -> dict[str, bool]:
    """
    Ping all handles in the pool and return their health status.

    Returns:
        A dict mapping handle_id to bool (True = healthy, False = dead).
        Dead handles are removed from the pool.
    """
```

#### 4.3.4 `drain`

```python
async def drain(self, timeout: int = 30) -> None:
    """
    Gracefully shut down all handles.

    Sends a shutdown signal to each subprocess and waits up to `timeout`
    seconds for clean exit. Subprocesses that do not exit in time are
    forcefully killed.
    """
```

#### 4.3.5 `kill`

```python
async def kill(self, handle_id: str) -> None:
    """
    Forcefully terminate a single subprocess (SIGKILL).

    Used for unresponsive handles that fail health checks.
    """
```

### 4.4 LRU Eviction

The pool uses an `OrderedDict` to track usage order. On `acquire`:

1. If a matching handle is found, move it to the end (most recently used).
2. If the pool exceeds `max_size` after acquiring, remove the first item (least recently used).
3. If no matching handle exists, spawn a new subprocess (if under `max_size`).
4. If at `max_size` and no match, block until a handle is released.

### 4.5 Health Checking

A background task runs every 60 seconds:

```python
async def _health_check_loop(self):
    while True:
        await asyncio.sleep(60)
        health = await self.health_check()
        for handle_id, is_healthy in health.items():
            if not is_healthy:
                await self.kill(handle_id)
                logger.warning(f"Removed unhealthy handle: {handle_id}")
```

### 4.6 Lifecycle: Spawn / Drain / Kill

| Phase | Trigger | Action |
|-------|---------|--------|
| **Spawn** | `acquire()` with no warm match | Fork subprocess, send init config, wait for "ready" signal |
| **Release** | `release(handle)` | Mark idle, update LRU timestamp |
| **Evict** | Pool exceeds `max_size` or `idle_timeout` exceeded | Send SIGTERM, wait 5s, SIGKILL if needed |
| **Drain** | Service shutdown | Graceful shutdown of all handles (timeout 30s) |
| **Kill** | Health check failure or `kill(handle_id)` | Immediate SIGKILL, remove from pool |

---

## 5. OrchestrationService

### 5.1 Purpose

The `OrchestrationService` is the central coordinator for multi-agent tasks. It handles **routing** (which agent gets the task), **cancellation** (propagating cancel signals), and **cost tracking** (recording token usage per task).

### 5.2 Class Definition

```python
class OrchestrationService:
    def __init__(
        self,
        pool: ProcessPool,
        router: AgentRouter,
        cancellation_registry: CancellationRegistry,
        cost_manager: CostManager,
    ):
        self.pool = pool
        self.router = router
        self.cancellation_registry = cancellation_registry
        self.cost_manager = cost_manager
```

### 5.3 Core Methods

#### 5.3.1 `dispatch`

```python
async def dispatch(self, task: A2ATask, target_card: AgentCard) -> A2ATaskResult:
    """
    Route a task to the appropriate adapter and return the result.

    Steps:
        1. Resolve target_card to an adapter instance via AgentRouter.
        2. Acquire a subprocess handle from ProcessPool.
        3. Register the task_id in CancellationRegistry.
        4. Invoke the adapter (invoke or stream based on task config).
        5. Record token usage in CostManager.
        6. Release the handle back to the pool.
        7. Return the A2ATaskResult.
    """
```

#### 5.3.2 `cancel`

```python
async def cancel(self, task_id: str) -> None:
    """
    Cancel an in-flight task.

    Steps:
        1. Look up the adapter and handle for the task in CancellationRegistry.
        2. Call adapter.cancel(task_id).
        3. If the adapter does not respond within 5s, kill the subprocess.
        4. Mark the task as CANCELED in the registry.
    """
```

#### 5.3.3 `get_cost`

```python
async def get_cost(self, task_id: str) -> CostRecord:
    """
    Return the cost record for a completed or in-flight task.

    The CostRecord includes:
        - input_tokens, output_tokens
        - cost_usd (computed from token prices)
        - model used
        - duration
    """
```

### 5.4 AgentRouter

The `AgentRouter` maps a task's required skills to a concrete adapter instance:

```python
class AgentRouter:
    def __init__(self, registry: FrameworkAdapterRegistry):
        self.registry = registry

    def resolve(self, target_card: AgentCard) -> AgentWrapper:
        """
        Find the adapter instance that matches the target AgentCard.

        Matching is done by card.name and card.version. If multiple
        adapters match, the one with the most available pool handles
        is preferred (load balancing).
        """
```

### 5.5 CancellationRegistry

The `CancellationRegistry` tracks active tasks and their associated adapters/handles:

```python
class CancellationRegistry:
    def register(self, task_id: str, adapter: AgentWrapper, handle: SubprocessHandle): ...
    def unregister(self, task_id: str): ...
    def get(self, task_id: str) -> tuple[AgentWrapper, SubprocessHandle] | None: ...
```

---

## 6. Cost Management

### 6.1 Purpose

LLM API calls cost money. The `CostManager` tracks token usage per task, per endpoint, and per organization, enforcing **budget caps** to prevent runaway costs.

### 6.2 Per-Endpoint Caps

Each managed endpoint (device) can have a monthly token budget. The `CostManager` checks the budget before dispatching a task:

```python
class CostManager:
    async def check_budget(self, endpoint_id: str, estimated_tokens: int) -> bool:
        """
        Return True if the endpoint has sufficient budget for the estimated tokens.
        Return False if the budget is exhausted.
        """
```

If the budget is exhausted, the task is rejected with `TaskState.FAILED` and reason `"budget_exceeded"`.

### 6.3 Token Usage Tracking

After each task completes, the adapter reports token usage to the `CostManager`:

```python
class CostRecord:
    task_id: str
    endpoint_id: str
    model: str
    input_tokens: int
    output_tokens: int
    cost_usd: float
    timestamp: datetime
```

The `TokenCounter` component estimates token counts for streaming responses (in case the provider does not report them in the final message).

### 6.4 Budget Alerts

When an endpoint's usage reaches 80%, 90%, and 100% of its budget, the `CostManager` emits alerts:

| Threshold | Alert Type | Recipient |
|-----------|-----------|-----------|
| 80% | Warning notification | Org admin email |
| 90% | Warning notification | Org admin email + dashboard banner |
| 100% | Budget exhausted notification | All admins + task rejection enabled |

Alerts are delivered via the `NotificationDispatcher` (email, Slack, webhook).

---

## 7. Human-in-the-Loop

### 7.1 Purpose

LLM agents can propose actions that are risky or require human judgment (e.g., "Approve patch KB5034441 on 500 endpoints"). The A2A protocol defines a special task state — `INPUT_REQUIRED` — for these cases. The `HITLService` manages the approval workflow.

### 7.2 A2A INPUT_REQUIRED State Handling

When an adapter determines that a task needs human input, it returns:

```python
A2ATaskResult(
    task_id=task_id,
    state=TaskState.INPUT_REQUIRED,
    artifacts=[Artifact(
        parts=[DataPart(value={
            "prompt": "Approve patch KB5034441 on 500 endpoints?",
            "options": ["approve", "reject"],
            "context": {...},
        })],
    )],
)
```

The `OrchestrationService` detects this state and creates a pending approval record in the database. The task is **paused** (the subprocess is released back to the pool, but the task state is persisted).

### 7.3 Approval UI Integration

The React frontend subscribes to `INPUT_REQUIRED` tasks via SSE. When a task enters this state, the dashboard displays an **approval modal** with:

- The prompt text
- Available options (approve/reject/custom input)
- Context (which agent requested it, what the proposed action is)
- A timeout (default: 24 hours, configurable)

The user clicks approve or reject, which sends an HTTP request to:

```
POST /a2a/v1/approvals/{id}/approve
POST /a2a/v1/approvals/{id}/reject
```

### 7.4 Notification Delivery

The `NotificationDispatcher` sends notifications for pending approvals via:

| Channel | Use Case |
|---------|----------|
| Email | All approvers |
| Slack | Org-configured channel |
| WebSocket | Dashboard real-time push |
| Mobile push (future) | On-call responders |

Notifications include a deep link to the approval modal.

### 7.5 ApprovalStateMachine

The approval state machine tracks the lifecycle of a pending approval:

```
PENDING → APPROVED → TASK_RESUMED → COMPLETED
   ↓
REJECTED → TASK_RESUMED → COMPLETED (with rejection context)
   ↓
TIMEOUT → AUTO_REJECTED → TASK_CANCELED
```

If the approval times out (24h default), the task is auto-rejected and the agent resumes with a rejection context.

### 7.6 Resuming a Task

When an approval is received, the `HITLService` reconstructs the task context from the database and re-dispatches it to the adapter with the approval result included in the message:

```python
async def resume_task(self, task_id: str, approval_result: str):
    task = await self.db.get_task(task_id)
    task.context["human_response"] = approval_result
    task.state = TaskState.WORKING
    await self.db.update_task(task)
    await self.orchestration.dispatch(task, task.target_card)
```

---

## 8. Implementation Steps

The subsystem is built across **16 tasks** (from `MASTER_IMPLEMENTATION_PLAN.md` §3.3.5). Each task produces concrete files in the `adapters/` package.

### Task Summary Table

| Task | Component | Files | Est. Steps |
|------|-----------|-------|-----------|
| 1 | Project skeleton, config, conftest | 5 | 6 |
| 2 | Database engine + 6 SQLModel classes | 11 | 12 |
| 3 | Adapter exception taxonomy (10 classes) + TaskStateMachine | 5 | 8 |
| 4 | AgentWrapper ABC + FrameworkAdapterRegistry | 4 | 5 |
| 5 | LangGraph adapter | 5 | 10 |
| 6 | CrewAI adapter | 4 | 8 |
| 7 | AutoGen adapter | 4 | 9 |
| 8 | Semantic Kernel adapter | 4 | 9 |
| 9 | OpenAI Agents SDK adapter | 4 | 8 |
| 10 | Anthropic Claude adapter | 4 | 8 |
| 11 | ProcessPool (warm pool, LRU, health) | 8 | 12 |
| 12 | OrchestrationService, AgentRouter, CancellationRegistry, CostManager | 9 | 15 |
| 13 | TokenCounter + full cost test coverage | 4 | 6 |
| 14 | HITLService, ApprovalStateMachine, NotificationDispatcher | 8 | 10 |
| 15 | A2A JSON-RPC router, SSE streaming, FastAPI factory | 11 | 14 |
| 16 | Documentation + self-review | 4 | 8 |

### Detailed Task Descriptions

#### Task 1: Project Skeleton

- `adapters/pyproject.toml` — Python package metadata, dependencies
- `adapters/src/oap/__init__.py` — Package init
- `adapters/src/oap/config.py` — Pydantic settings (API keys, pool size, timeouts)
- `adapters/tests/conftest.py` — Pytest fixtures (mock adapters, sample messages)
- `adapters/.env.example` — Environment variable template

#### Task 2: Database Layer

- `adapters/src/oap/db/engine.py` — Async SQLAlchemy engine
- `adapters/src/oap/db/models.py` — 6 SQLModel classes:
  - `AgentProcess` (process state, last health check)
  - `Task` (A2A task persistence)
  - `Artifact` (artifact storage)
  - `CostRecord` (token usage and cost)
  - `ApprovalRequest` (HITL pending approvals)
  - `Skill` (registered skills per adapter)
- `adapters/src/oap/db/migrations/` — Alembic migrations
- `adapters/src/oap/db/repository.py` — CRUD helpers

#### Task 3: Exception Taxonomy

- `adapters/src/oap/exceptions.py` — 10 exception classes:
  - `AdapterError` (base)
  - `TaskNotFoundError`
  - `TaskAlreadyTerminal`
  - `BudgetExceededError`
  - `ProcessSpawnError`
  - `ProcessHealthCheckFailed`
  - `FrameworkImportError`
  - `StreamingNotSupported`
  - `InvalidMessageFormat`
  - `TimeoutError`
- `adapters/src/oap/state_machine.py` — `TaskStateMachine` class enforcing valid transitions

#### Task 4: AgentWrapper ABC

- `adapters/src/oap/agent_wrapper.py` — ABC definition (see §2)
- `adapters/src/oap/registry.py` — `FrameworkAdapterRegistry` for adapter discovery
- `adapters/src/oap/a2a_types.py` — `A2AMessage`, `A2ATaskResult`, `A2ADeltaEvent`, `AgentCard`, `AgentSkill`, `Artifact`, `Part` types
- `adapters/tests/test_agent_wrapper.py` — Interface compliance tests

#### Task 5: LangGraph Adapter

- `adapters/src/oap/adapters/langgraph/__init__.py`
- `adapters/src/oap/adapters/langgraph/adapter.py` — `LangGraphAdapter` class
- `adapters/src/oap/adapters/langgraph/state_translator.py` — Message ↔ state dict
- `adapters/src/oap/adapters/langgraph/artifact_extractor.py` — State → Artifact extraction
- `adapters/tests/adapters/langgraph/` — Unit tests (10 test cases)

#### Task 6: CrewAI Adapter

- `adapters/src/oap/adapters/crewai/__init__.py`
- `adapters/src/oap/adapters/crewai/adapter.py` — `CrewAIAdapter` class
- `adapters/src/oap/adapters/crewai/peer_registry.py` — A2A peer registration
- `adapters/tests/adapters/crewai/` — Unit tests (8 test cases)

#### Task 7: AutoGen Adapter

- `adapters/src/oap/adapters/autogen/__init__.py`
- `adapters/src/oap/adapters/autogen/adapter.py` — `AutoGenAdapter` class
- `adapters/src/oap/adapters/autogen/termination_mapper.py` — Termination → TaskState
- `adapters/tests/adapters/autogen/` — Unit tests (9 test cases)

#### Task 8: Semantic Kernel Adapter

- `adapters/src/oap/adapters/semantic_kernel/__init__.py`
- `adapters/src/oap/adapters/semantic_kernel/adapter.py` — `SemanticKernelAdapter` class
- `adapters/src/oap/adapters/semantic_kernel/maf_host.py` — MAF A2A hosting
- `adapters/tests/adapters/semantic_kernel/` — Unit tests (9 test cases)

#### Task 9: OpenAI Agents SDK Adapter

- `adapters/src/oap/adapters/openai/__init__.py`
- `adapters/src/oap/adapters/openai/adapter.py` — `OpenAIAdapter` class
- `adapters/src/oap/adapters/openai/mcp_bridge.py` — MCP tool integration
- `adapters/tests/adapters/openai/` — Unit tests (8 test cases)

#### Task 10: Anthropic Claude Adapter

- `adapters/src/oap/adapters/anthropic/__init__.py`
- `adapters/src/oap/adapters/anthropic/adapter.py` — `AnthropicAdapter` class
- `adapters/src/oap/adapters/anthropic/tool_executor.py` — Tool call execution
- `adapters/tests/adapters/anthropic/` — Unit tests (8 test cases)

#### Task 11: ProcessPool

- `adapters/src/oap/pool/__init__.py`
- `adapters/src/oap/pool/process_pool.py` — Main `ProcessPool` class
- `adapters/src/oap/pool/handle.py` — `SubprocessHandle` class
- `adapters/src/oap/pool/health.py` — Health check loop
- `adapters/src/oap/pool/eviction.py` — LRU eviction logic
- `adapters/src/oap/pool/lifecycle.py` — Spawn/drain/kill state machine
- `adapters/tests/pool/` — Unit tests (12 test cases)
- `adapters/tests/integration/test_pool_lifecycle.py` — Integration test

#### Task 12: OrchestrationService

- `adapters/src/oap/orchestration/__init__.py`
- `adapters/src/oap/orchestration/service.py` — `OrchestrationService` class
- `adapters/src/oap/orchestration/router.py` — `AgentRouter` class
- `adapters/src/oap/orchestration/cancellation.py` — `CancellationRegistry` class
- `adapters/src/oap/orchestration/cost_manager.py` — `CostManager` class
- `adapters/src/oap/orchestration/dispatcher.py` — Task dispatch logic
- `adapters/tests/orchestration/` — Unit tests
- `adapters/tests/integration/test_dispatch_flow.py` — End-to-end dispatch
- `adapters/tests/integration/test_cancellation.py` — Cancellation propagation

#### Task 13: TokenCounter

- `adapters/src/oap/orchestration/token_counter.py` — Token estimation
- `adapters/src/oap/orchestration/pricing.py` — Model price tables
- `adapters/tests/orchestration/test_cost_tracking.py` — Cost calculation tests
- `adapters/tests/orchestration/test_budget_enforcement.py` — Budget cap tests

#### Task 14: HITLService

- `adapters/src/oap/hitl/__init__.py`
- `adapters/src/oap/hitl/service.py` — `HITLService` class
- `adapters/src/oap/hitl/approval_state_machine.py` — State transitions
- `adapters/src/oap/hitl/notification_dispatcher.py` — Multi-channel notifications
- `adapters/src/oap/hitl/templates/` — Email/Slack templates
- `adapters/tests/hitl/` — Unit tests
- `adapters/tests/integration/test_hitl_flow.py` — End-to-end HITL
- `adapters/tests/integration/test_approval_timeout.py` — Timeout auto-reject

#### Task 15: A2A JSON-RPC Router

- `adapters/src/oap/api/__init__.py`
- `adapters/src/oap/api/router.py` — JSON-RPC method dispatch
- `adapters/src/oap/api/sse.py` — Server-Sent Events streaming
- `adapters/src/oap/api/factory.py` — FastAPI app factory
- `adapters/src/oap/api/middleware.py` — Auth, CORS, logging
- `adapters/src/oap/api/models.py` — Pydantic request/response models
- `adapters/src/oap/a2a_server.py` — A2A server entry point
- `adapters/tests/api/` — HTTP endpoint tests
- `adapters/tests/integration/test_jsonrpc.py` — JSON-RPC compliance
- `adapters/tests/integration/test_sse_streaming.py` — SSE streaming
- `adapters/tests/integration/test_agent_card_discovery.py` — Card/skill queries

#### Task 16: Documentation

- `docs/architecture/AGENT_FRAMEWORKS.md` — This document
- `docs/architecture/POOL_LIFECYCLE.md` — ProcessPool deep dive
- `docs/architecture/HITL_FLOW.md` — Human-in-the-loop diagrams
- `adapters/README.md` — Developer quickstart

---

## Appendix A: Directory Structure

```
adapters/
├── pyproject.toml
├── .env.example
├── src/
│   └── oap/
│       ├── __init__.py
│       ├── config.py
│       ├── agent_wrapper.py        # AgentWrapper ABC
│       ├── registry.py             # FrameworkAdapterRegistry
│       ├── a2a_types.py            # A2A protocol types
│       ├── a2a_server.py           # A2A server entry point
│       ├── state_machine.py        # TaskStateMachine
│       ├── exceptions.py           # Exception taxonomy
│       ├── adapters/
│       │   ├── langgraph/
│       │   ├── crewai/
│       │   ├── autogen/
│       │   ├── semantic_kernel/
│       │   ├── openai/
│       │   └── anthropic/
│       ├── pool/
│       │   ├── process_pool.py
│       │   ├── handle.py
│       │   ├── health.py
│       │   ├── eviction.py
│       │   └── lifecycle.py
│       ├── orchestration/
│       │   ├── service.py
│       │   ├── router.py
│       │   ├── cancellation.py
│       │   ├── cost_manager.py
│       │   ├── dispatcher.py
│       │   ├── token_counter.py
│       │   └── pricing.py
│       ├── hitl/
│       │   ├── service.py
│       │   ├── approval_state_machine.py
│       │   ├── notification_dispatcher.py
│       │   └── templates/
│       ├── api/
│       │   ├── router.py
│       │   ├── sse.py
│       │   ├── factory.py
│       │   ├── middleware.py
│       │   └── models.py
│       └── db/
│           ├── engine.py
│           ├── models.py
│           ├── repository.py
│           └── migrations/
└── tests/
    ├── conftest.py
    ├── adapters/
    ├── pool/
    ├── orchestration/
    ├── hitl/
    ├── api/
    └── integration/
```

---

## Appendix B: Configuration Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `OAP_POOL_MAX_SIZE` | 10 | Maximum warm subprocesses |
| `OAP_POOL_IDLE_TIMEOUT` | 300 | Seconds before LRU eviction |
| `OAP_POOL_HEALTH_INTERVAL` | 60 | Seconds between health checks |
| `OAP_DISPATCH_TIMEOUT` | 120 | Max seconds per task |
| `OAP_CANCEL_TIMEOUT` | 5 | Max seconds to wait for graceful cancel |
| `OAP_BUDGET_DEFAULT` | 1000000 | Default monthly token budget per endpoint |
| `OAP_BUDGET_ALERT_THRESHOLDS` | [0.8, 0.9, 1.0] | Budget alert thresholds |
| `OAP_HITL_TIMEOUT` | 86400 | Seconds before auto-reject (24h) |
| `OAP_FRAMEWORK_ENABLED` | all | Comma-separated list of enabled adapters |

---

## Appendix C: Adapter Capability Matrix

| Capability | LangGraph | CrewAI | AutoGen | Semantic Kernel | OpenAI | Anthropic |
|------------|-----------|--------|---------|-----------------|--------|-----------|
| Streaming | Yes | Yes | Yes | Yes | Yes | Yes |
| Cancellation | Yes (cooperative) | Yes | Yes (event) | Yes (thread) | Yes (runner) | Yes (asyncio) |
| Multi-agent | Yes (graph) | Yes (crew) | Yes (group chat) | Yes (handoff) | Yes (handoff) | No (single agent) |
| Tool calling | Yes (tools) | Yes (tools) | Yes (functions) | Yes (plugins) | Yes (function_tool) | Yes (tool_use) |
| State persistence | Yes (checkpointer) | No | No | Yes (thread) | No | No |
| Native A2A | Yes (LangSmith) | Yes (crewai.a2a) | No | Yes (MAF) | No | No |
| MCP integration | Via custom tool | Via custom tool | Via custom function | Via custom plugin | Yes (bridge) | Via custom tool |

---

*End of document.*
