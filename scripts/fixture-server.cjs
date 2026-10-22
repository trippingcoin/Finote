// Observe Node's real HTTP listener; do not replace requests or responses.
const net=require('node:net');
const path=require('node:path');
const names=new Set(['llm-errors','telegram-outbox','telegram-context','panel-timeout']);
if(!names.has(process.argv[2]))throw Error('Unknown fixture');
const listen=net.Server.prototype.listen;
net.Server.prototype.listen=function(){
 this.once('listening',()=>process.send?.({port:this.address().port}));
 return listen.call(this,0,'127.0.0.1');
};
require(path.join(__dirname,'../postman/mocks',process.argv[2],'default.js'));
