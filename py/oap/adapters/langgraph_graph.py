"""
Graph / LLM construction helpers for the LangGraph adapter.

Contains the framework-specific wiring — building the framework-native chat
model and compiling a minimal state graph — factored out of the adapter class
so the adapter code stays readable.
"""

from __future__ import annotations

from typing import Any

from oap.adapters.errors import InvocationError
from oap.adapters.types import Part

# Models this adapter supports.
HEALTHY_COST_MODELS: list[str] = [
    # Anthropic
    "claude-opus-4-20250514",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
    # OpenAI
    "gpt-4o",
    "gpt-4o-mini",
    "o3",
    "o4-mini",
    # Google
    "gemini-2.5-pro",
    "gemini-2.0-flash",
]


def _build_llm(model: str, max_tokens: int, temperature: float) -> Any:
    """Construct the framework-native chat model for the configured provider."""
    if model.startswith("claude"):
        from langchain_anthropic import ChatAnthropic

        return ChatAnthropic(
            model=model,
            max_tokens=max_tokens,
            temperature=temperature,
        )
    elif model.startswith("gpt") or model.startswith("o"):
        from langchain_openai import ChatOpenAI

        return ChatOpenAI(
            model=model,
            max_tokens=max_tokens,
            temperature=temperature,
        )
    elif model.startswith("gemini"):
        from langchain_google_genai import ChatGoogleGenerativeAI

        return ChatGoogleGenerativeAI(
            model=model,
            max_output_tokens=max_tokens,
            temperature=temperature,
        )
    else:
        raise InvocationError(
            f"Unsupported model for LangGraph adapter: {model}",
            adapter_name="langgraph",
        )


def _build_graph(model: str, max_tokens: int, temperature: float) -> Any:
    """Compile a minimal two-node graph (agent -> END)."""
    from typing import TypedDict

    from langgraph.graph import END, StateGraph
    from langchain_core.messages import BaseMessage

    class GraphState(TypedDict):
        messages: list[BaseMessage]

    llm = _build_llm(model, max_tokens, temperature)

    def agent_node(state: GraphState) -> dict:
        response = llm.invoke(state["messages"])
        return {"messages": state["messages"] + [response]}

    builder = StateGraph(GraphState)
    builder.add_node("agent", agent_node)
    builder.set_entry_point("agent")
    builder.add_edge("agent", END)
    return builder.compile()


def _parts_to_messages(system_message: str, parts: list[Part]) -> list[Any]:
    from langchain_core.messages import HumanMessage, SystemMessage

    msgs: list[Any] = [SystemMessage(content=system_message)]
    for p in parts:
        if p.type == "text" and p.text:
            msgs.append(HumanMessage(content=p.text))
    return msgs
