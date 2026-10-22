// Minimal responses from Finote's JSON error contract plus a delayed upstream.
const http = require('http');
http.createServer((req, res) => {
  if (req.method !== 'POST') { res.writeHead(501); res.end(); return; }
  if (req.url === '/slow') {
    setTimeout(() => {
      res.writeHead(201, {'Content-Type': 'application/json'});
      res.end('{"id":"test-id"}');
    }, 500);
    return;
  }
  if (req.url === '/error') {
    res.writeHead(503, {'Content-Type': 'application/json'});
    res.end('{"error":"Время ожидания истекло.","code":"request_timeout"}'); return;
  }
  res.writeHead(501); res.end();
}).listen(process.env.PORT);
