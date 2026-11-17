"""Solvers that send to the model what the Go runtime sends for each role.

The model of a role comes from the Inspect model role of the same name, for example
`--model-role author=openrouter/anthropic/claude-sonnet-5.5`. A role that is not bound uses
the model of `--model`. The Ask feature uses the model of the explainer role, as in Go.
"""

from inspect_ai.model import (
    ChatMessage,
    ChatMessageSystem,
    ChatMessageTool,
    ChatMessageUser,
    Model,
    get_model,
)
from inspect_ai.solver import Generate, Solver, TaskState, solver

from clanker_evals.author_tools import MAX_STEPS, AuthorSession, author_tools
from clanker_evals.gotext import scrub
from clanker_evals.repo import load_repos, prompt

# Role -> (prompt file, Inspect model role). DefaultModels in agent.go names the models.
ROLES: dict[str, tuple[str, str]] = {
    "author": ("author", "author"),
    "explainer": ("explainer", "explainer"),
    "investigator": ("investigator", "investigator"),
    "ask": ("ask", "explainer"),
    "reviewer": ("reviewer", "reviewer"),
}
TRACE_KEY = "author_trace"


def model_for(role: str) -> Model:
    """Return the model of a role: its Inspect model role, or the model of --model."""
    return get_model(role=ROLES[role][1])


def _scrub_messages(messages: list[ChatMessage]) -> None:
    """Scrub the text of every message in place, as Agent.complete does before each call."""
    for message in messages:
        if isinstance(message.content, str):
            message.content = scrub(message.content)


def _opening(role: str, user_text: str) -> list[ChatMessage]:
    return [
        ChatMessageSystem(content=prompt(ROLES[role][0])),
        ChatMessageUser(content=user_text),
    ]


@solver
def role_turn(role: str | None = None) -> Solver:
    """Send one request for the Explainer, the Investigator, Ask or the Reviewer.

    Args:
        role: explainer, investigator, ask or reviewer. None takes the role from the key
            "role" of the sample metadata.
    """

    async def solve(state: TaskState, generate: Generate) -> TaskState:
        sample_role = role or state.metadata["role"]
        if sample_role == "author":
            return await _author_loop(state)
        messages = _opening(sample_role, state.input_text)
        _scrub_messages(messages)
        output = await model_for(sample_role).generate(messages)
        state.messages = [*messages, output.message]
        state.output = output
        return state

    return solve


async def _author_loop(state: TaskState) -> TaskState:
    session = AuthorSession(
        repos=load_repos(state.metadata.get("overlay")),
        finding_key=state.metadata["finding_key"],
    )
    model = model_for("author")
    messages = _opening("author", state.input_text)
    for _ in range(MAX_STEPS):
        _scrub_messages(messages)
        output = await model.generate(messages, tools=author_tools())
        state.output = output
        messages.append(output.message)
        if not output.message.tool_calls:
            break
        for call in output.message.tool_calls:
            arguments = None if call.parse_error else call.arguments
            result = session.run(call.function, arguments)
            messages.append(
                ChatMessageTool(content=result, tool_call_id=call.id, function=call.function)
            )
    state.messages = messages
    state.store.set(TRACE_KEY, session.trace())
    return state


@solver
def author_loop() -> Solver:
    """Run the Author loop of Agent.Draft: at most eight requests with the three tools.

    The sample metadata gives "finding_key" and, optionally, "overlay" for load_repos. The
    trace of the tool calls goes to the sample store under TRACE_KEY.
    """

    async def solve(state: TaskState, generate: Generate) -> TaskState:
        return await _author_loop(state)

    return solve
