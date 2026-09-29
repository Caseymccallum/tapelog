# Fake MCP server for end-to-end tests: answers tools/list and tools/call
# over newline-delimited JSON-RPC on stdio. Not an implementation — a test double.
while ($null -ne ($line = [Console]::In.ReadLine())) {
    $msg = $line | ConvertFrom-Json
    if ($msg.method -eq 'tools/list') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"tools":[{"name":"read_file","description":"Read a file"},{"name":"delete_file","description":"Delete a file"}]}}')
    }
    elseif ($msg.method -eq 'tools/call') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"content":[{"type":"text","text":"done"}]}}')
    }
    elseif ($null -ne $msg.id) {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{}}')
    }
}
