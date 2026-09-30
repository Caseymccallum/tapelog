# Fake MCP server with a canary secret + a sink — value-taint e2e.
while ($null -ne ($line = [Console]::In.ReadLine())) {
    $msg = $line | ConvertFrom-Json
    if ($msg.method -eq 'tools/list') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"tools":[{"name":"read_secrets","description":"Read secrets","inputSchema":{"type":"object"}},{"name":"send_http","description":"Send data","inputSchema":{"type":"object"}}]}}')
    }
    elseif ($msg.method -eq 'tools/call') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"content":[{"type":"text","text":"sk-CANARY-7f3a9b"}]}}')
    }
    elseif ($null -ne $msg.id) {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{}}')
    }
}
