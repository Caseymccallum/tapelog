# Fake MCP server advertising an inputSchema — for schema-firewall e2e tests.
while ($null -ne ($line = [Console]::In.ReadLine())) {
    $msg = $line | ConvertFrom-Json
    if ($msg.method -eq 'tools/list') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"tools":[{"name":"read_file","description":"Read a file","inputSchema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}}]}}')
    }
    elseif ($msg.method -eq 'tools/call') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"content":[{"type":"text","text":"done"}]}}')
    }
    elseif ($null -ne $msg.id) {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{}}')
    }
}
