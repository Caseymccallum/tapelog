# Fake MCP server exposing resources + prompts — for surface-gating e2e.
while ($null -ne ($line = [Console]::In.ReadLine())) {
    $msg = $line | ConvertFrom-Json
    if ($msg.method -eq 'tools/list') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"tools":[{"name":"read_file","description":"Read","inputSchema":{"type":"object"}}]}}')
    }
    elseif ($msg.method -eq 'resources/read') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"contents":[{"uri":"x","text":"resource-content"}]}}')
    }
    elseif ($msg.method -eq 'prompts/get') {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{"messages":[{"role":"user","content":{"type":"text","text":"prompt"}}]}}')
    }
    elseif ($null -ne $msg.id) {
        Write-Output ('{"jsonrpc":"2.0","id":' + $msg.id + ',"result":{}}')
    }
}
