// Chat Completions tool-call envelopes. Fault routes are test-only.
const http = require('http');
const counts = new Map();
http.createServer((req,res) => {
 const url = new URL(req.url,'http://localhost');
 if (req.method==='GET' && url.pathname==='/counts') {
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({count:counts.get(url.searchParams.get('input'))||0}));return;
 }
 if(req.method!=='POST' || !url.pathname.endsWith('/chat/completions')) {res.writeHead(501);res.end();return;}
 let raw='';req.on('data',chunk=>raw+=chunk);req.on('end',()=>{
  let body;try{body=JSON.parse(raw);}catch{res.writeHead(400);res.end('{}');return;}
  if(!body.model || body.response_format || body.tool_choice!=='required' || body.parallel_tool_calls!==false || body.tools?.length!==7 || body.messages?.[0]?.role!=='system') {res.writeHead(400);res.end('{}');return;}
  const input=body.messages.find(m=>m.role==='user')?.content;
  const count=(counts.get(input)||0)+1;counts.set(input,count);
  const mode=url.pathname.split('/')[1];
  console.log(JSON.stringify({event:'request',member:'llm-errors',mode,count}));
  if(mode==='always503'||(mode==='retry'&&count===1)){res.writeHead(503,{'Content-Type':'text/html'});res.end('<html>unavailable</html>');return;}
  if(['401','403','404','429','500'].includes(mode)){res.writeHead(Number(mode));res.end('{"error":"test error"}');return;}
  if(mode==='slow'){const timer=setTimeout(()=>res.end('{}'),2000);res.on('close',()=>clearTimeout(timer));return;}
  if(mode==='invalid-envelope'){res.end('<html>not JSON</html>');return;}
  if(mode==='empty'){res.end('{"choices":[]}');return;}
  let args={category:'еда',amount:400,comment:'кофе'};
  if(mode==='foreign')args.user_id='20';
  if(mode==='negative')args.amount=-400;
  if(mode==='legacy-action')args.action='create';
  if(mode==='null')args.amount=null;
  let arguments=JSON.stringify(args);
  if(mode==='invalid-json')arguments='{';
  if(mode==='extra-json')arguments+=' {}';
  const call={id:'call_1',type:'function',function:{name:mode==='action'?'sql':'create_payment',arguments}};
  let message={role:'assistant',content:null,tool_calls:mode==='multiple'?[call,{...call,id:'call_2'}]:[call]};
  if(mode==='legacy')message={role:'assistant',content:JSON.stringify({action:'create',...args})};
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{finish_reason:mode==='truncated'?'length':'tool_calls',message}]}));
 });
}).listen(process.env.PORT);
