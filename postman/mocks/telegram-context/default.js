// Local fixture for the Chat Completions wire format used by Agent.planOnce.
const http = require('http');
http.createServer((req, res) => {
 let raw = '';
 req.on('data', chunk => { raw += chunk; });
 req.on('end', () => {
  try {
   const body = JSON.parse(raw);
   if (req.method !== 'POST' || req.url !== '/chat/completions' || body.response_format.type !== 'json_object' || !body.model) throw Error('contract');
   const text = body.messages.find(m => m.role === 'user').content;
   const fixtures = {
    'добавь кофе': {action:'draft_create',category:'еда',comment:'кофе'},
    'измени второй на 700': {action:'update',list_index:2,amount:700},
    'а id?': {action:'id',selected:true}
   };
   if (!fixtures[text]) throw Error('unknown fixture');
   res.setHeader('Content-Type','application/json');
   res.end(JSON.stringify({choices:[{message:{content:JSON.stringify(fixtures[text])}}]}));
  } catch (_) {res.writeHead(400);res.end('{}');}
 });
}).listen(process.env.PORT);
