# gigguide-mcp

An MCP server for searching upcoming gigs and live music events from the [Rock Regeneration gig guide](https://www.rock-regeneration.co.uk/gig-guide/), covering venues across southern England (Hampshire, Dorset, Wiltshire, Isle of Wight, etc).

## Usage

Add to your Claude config:

```json
{
  "mcpServers": {
    "gigguide": {
      "command": "docker",
      "args": ["run", "--rm", "-i", "ghcr.io/cuotos/gigguide-mcp"]
    }
  }
}
```

Or with the Claude CLI:

```bash
claude mcp add --transport stdio gigguide -- docker run --rm -i ghcr.io/cuotos/gigguide-mcp
```

## Tools

### `search_gigs`

Search for upcoming gigs. All filters are optional — omit to return everything.

| Parameter   | Type   | Description                                      |
|-------------|--------|--------------------------------------------------|
| `location`  | string | Town or city (expands to nearby towns)           |
| `artist`    | string | Artist or band name (partial, case-insensitive)  |
| `venue`     | string | Venue name (partial, case-insensitive)           |
| `from_date` | string | Start date filter — `YYYY-MM-DD`                 |
| `to_date`   | string | End date filter — `YYYY-MM-DD`                   |

## Testing

Build the image locally:

```bash
docker build -t gigguide-mcp .
```

Test it responds correctly:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}' \
  | docker run --rm -i gigguide-mcp
```

Expected output:

```json
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{}},"protocolVersion":"2024-11-05","serverInfo":{"name":"gigguide-mcp","version":"..."}}}
```

## Publishing

Images are published to `ghcr.io/cuotos/gigguide-mcp` automatically when a version tag is pushed:

```bash
git tag v1.0.0
git push origin v1.0.0
```
