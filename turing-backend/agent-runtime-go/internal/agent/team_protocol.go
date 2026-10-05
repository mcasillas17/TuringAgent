package agent

// TeamProtocolVersion is the agent-team protocol GeneralAssistant honors. At
// 1 it runs a job's agent_profile in place of the persona, caps tool calls at
// the profile's max_tool_calls, offers and dispatches only an enforced
// selected_tools set, skips automatic recall when asked, and replays history
// with a join's results framed as user-role data and every other role-system
// message omitted. A worker advertises it only for this executor.
const TeamProtocolVersion = 1
