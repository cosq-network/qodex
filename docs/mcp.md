# MCP Integration

Qodex can load tools from Model Context Protocol (MCP) servers over stdio or Streamable HTTP.

Configure servers in `.qodex/config.toml` or the user configuration:

```toml
[mcp.servers.files]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/project"]
enabled = true

[mcp.servers.files.env]
MODE = "readonly"

# Optional: keep the token out of this file. Qodex reads FILES_TOKEN from
# its own environment and passes it to the child as MCP_ACCESS_TOKEN.
[mcp.servers.files.auth]
type = "bearer" # none, bearer, or api_key
token_env = "FILES_TOKEN"
pass_env = "MCP_ACCESS_TOKEN"
```

For a remote Streamable HTTP server:

```toml
[mcp.servers.github]
transport = "streamable-http"
url = "https://mcp.example.com/mcp"
trust = "ask" # ask, trusted, or blocked

[mcp.servers.github.auth]
type = "oauth" # oauth uses an already-issued bearer token from token_env
token_env = "GITHUB_MCP_TOKEN"

[mcp.servers.github.permissions]
search = "allow"
create_issue = "ask"
delete_repository = "deny"
```

`transport` defaults to `stdio`. Streamable HTTP requests include the negotiated `MCP-Protocol-Version` and `Mcp-Session-Id` headers, and accept JSON or server-sent-event responses. The legacy HTTP+SSE transport is not enabled.

Each discovered MCP tool is exposed to the model as `mcp_<server>_<tool>`. MCP tools are classified as `network`, so they use Qodex’s normal network approval policy. Tool results are persisted in the same session and tool-event store as built-in tools.

MCP servers are started when a Qodex agent runtime starts and stopped when that runtime closes. If a configured server cannot initialize or does not return a tool list, Qodex fails startup with the server name and error.

Use the diagnostics commands before starting an agent:

```sh
qodex mcp list
qodex mcp doctor
qodex mcp doctor files
```

Diagnostics check whether the command is installed, whether configured authentication is available, whether the server completes MCP initialization and `ping`, and whether `tools/list` succeeds. Successful diagnostics report the protocol version, server identity, tool count, and advertised capabilities. Failures include an installation or configuration hint.

Authentication is environment-backed by design. `token_env` names a variable in Qodex’s environment; `pass_env` names the variable exposed to a stdio child process and defaults to `token_env`. For HTTP, `bearer` and `oauth` send `Authorization: Bearer ...`; `api_key` sends the token using `header` or `X-API-Key` by default. Secrets are never written to the TOML config or included in diagnostic output. Qodex does not perform browser-based OAuth login or dynamic client registration yet; obtain the access token with your organization’s OAuth flow and place it in `token_env`.

Trust defaults to `ask` for enabled servers. `trusted` starts a server without a prompt, while `blocked` skips it. Per-tool permissions use `allow`, `ask`, or `deny` and override the global network approval policy; deny rules remain effective even with `--yes`.

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
