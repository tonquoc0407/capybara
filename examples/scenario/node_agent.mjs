// Example subprocess adapter; replace decide() with a call to your agent.
// The process has normal host permissions; mocks cover only protocol tools.
let raw = "";
for await (const chunk of process.stdin) raw += chunk;
const request = JSON.parse(raw);
const last = request.messages.at(-1);
let response;
if (last.role === "tool" && last.name === "weather") {
  response = { assistant: "Weather: " + last.json.forecast, cost_usd: 0 };
} else if ((last.content || "").toLowerCase().includes("weather")) {
  response = { tool_calls: [{ name: "weather", arguments: {} }], cost_usd: 0 };
} else {
  response = { assistant: "Acknowledged: " + (last.content || ""), cost_usd: 0 };
}
process.stdout.write(JSON.stringify(response));
