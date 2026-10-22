// Minimal Telegram sendMessage envelopes. Fault routes are test-only.
const http = require('http');
http.createServer((req, res) => {
 if (req.method !== 'POST' || !req.url.endsWith('/sendMessage')) {
   res.writeHead(501); res.end(); return;
 }
 if (req.url === '/error/sendMessage') {
   res.writeHead(503, {'Content-Type': 'text/html'});
   res.end('<html>Service unavailable</html>'); return;
 }
 res.writeHead(200, {'Content-Type': 'application/json'});
 res.end(JSON.stringify({ok: true, result: {message_id: 1}}));
}).listen(process.env.PORT);
