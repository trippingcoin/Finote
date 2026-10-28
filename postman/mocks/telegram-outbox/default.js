// Telegram SDK sends URL-encoded forms. Fault routes are test-only.
const http = require('http');
http.createServer((req, res) => {
 if (req.method !== 'POST' || !req.url.endsWith('/sendMessage')) {
   res.writeHead(501); res.end(); return;
 }
 let raw='';req.on('data',chunk=>raw+=chunk);req.on('end',()=>{
  const form=new URLSearchParams(raw);
  if (!req.headers['content-type']?.startsWith('application/x-www-form-urlencoded') || !/^[1-9][0-9]*$/.test(form.get('chat_id')||'') || !form.get('text')) {
   res.writeHead(400);res.end(JSON.stringify({ok:false,error_code:400}));return;
  }
  if (req.url.startsWith('/error/')) {
   console.log(JSON.stringify({event:'request',member:'telegram-outbox',result:'503'}));
   res.writeHead(503, {'Content-Type':'text/html'});res.end('<html>Service unavailable</html>');return;
  }
  console.log(JSON.stringify({event:'request',member:'telegram-outbox',result:'sent'}));
  res.writeHead(200, {'Content-Type':'application/json'});
  res.end(JSON.stringify({ok:true,result:{message_id:1}}));
 });
}).listen(process.env.PORT);
