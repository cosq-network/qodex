# MCP Integration

Qodex can load tools from Model Context Protocol (MCP) servers that speak the stdio transport.

Configure servers in `.qodex/config.toml` or the user configuration:

```toml
[mcp.servers.files]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/project"]
enabled = true

[mcp.servers.files.env]
MODE = "readonly"
```

Each discovered MCP tool is exposed to the model as `mcp_<server>_<tool>`. MCP tools are classified as `network`, so they use Qodex’s normal network approval policy. Tool results are persisted in the same session and tool-event store as built-in tools.

MCP servers are started when a Qodex agent runtime starts and stopped when that runtime closes. If a configured server cannot initialize or does not return a tool list, Qodex fails startup with the server name and error.

MCP provides capabilities, not authority. Qodex still applies path validation, skill allowlists, approval policy, and destructive-command restrictions to the rest of the agent runtime.

## Interoperable instructions

Qodex loads common repository instruction files in parent-to-child order:

- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.cursorrules`
- `.github/copilot-instructions.md`
- `.cursor/rules/*.mdc`

These files are context only. They do not bypass Qodex safety policy. Qodex’s native `.qodex/skills/` system remains the place for routable skills, tool allowlists, context budgets, and pre-approved scripts.
