// Deterministic tool calls for draft creation and remembered references.
const http = require('http');
http.createServer((req, res) => {
 let raw = '';
 req.on('data', chunk => { raw += chunk; });
 req.on('end', () => {
  try {
   const body = JSON.parse(raw);
   if (req.method !== 'POST' || req.url !== '/chat/completions' || body.response_format || body.tool_choice!=='required' || body.parallel_tool_calls!==false || body.tools?.length!==7 || !body.model) throw Error('contract');
   const text = body.messages.find(m => m.role === 'user').content;
   const fixtures = {
    'добавь кофе': ['create_payment',{category:'еда',comment:'кофе'}],
    'измени второй на 700': ['update_payment',{list_index:2,amount:700}],
    'а id?': ['get_payment_id',{selected:true}],
    'кофе 350': ['create_payment',{category:'еда',amount:350,comment:'кофе'}],
    'такси 1200 вчера': ['create_payment',{category:'транспорт',amount:1200,comment:'такси',spent_at:'2026-10-27T12:00:00+05:00'}],
    'покажи траты': ['list_payments',{}],
    'сколько потратил на еду': ['sum_payments',{filter_category:'еда'}],
    'последнее было не 1200, а 450': ['update_payment',{latest:true,amount:450}],
    'удали такси': ['delete_payment',{search:'такси'}],
    'что-то': ['ask_clarification',{question:'Какое действие выполнить?'}]
   };
   if (!fixtures[text]) throw Error('unknown fixture');
   const [name,args]=fixtures[text];
   console.log(JSON.stringify({event:'request',member:'telegram-context',tool:name}));
   res.setHeader('Content-Type','application/json');
   res.end(JSON.stringify({choices:[{finish_reason:'tool_calls',message:{role:'assistant',content:null,tool_calls:[{id:'call_1',type:'function',function:{name,arguments:JSON.stringify(args)}}]}}]}));
  } catch (_) {res.writeHead(400);res.end('{}');}
 });
}).listen(process.env.PORT);
