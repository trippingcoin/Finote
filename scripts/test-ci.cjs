// Run every test against an isolated DB and the same HTTP fixtures as Postman.
// No service tokens, Postman account or external API calls are needed.
const {spawn} = require('node:child_process');
const path = require('node:path');
const root = path.resolve(__dirname,'..');
const children = [];
const env = {...process.env};
async function fixture(name,key) {
  // Port 0 avoids collisions. Child reports its actual bound port via IPC.
  const child=spawn(process.execPath,['scripts/fixture-server.cjs',name],{cwd:root,env,stdio:['ignore','inherit','inherit','ipc']});
  children.push(child);
  const port=await new Promise((resolve,reject)=>{
    const timer=setTimeout(()=>reject(new Error(name+' startup timeout')),5000);
    child.once('message',message=>{clearTimeout(timer);resolve(message.port);});
    child.once('error',error=>{clearTimeout(timer);reject(error);});
    child.once('exit',code=>{clearTimeout(timer);reject(new Error(name+' exited: '+code));});
  });
  env[key]='http://127.0.0.1:'+port;
}
function run(command,args) {
 return new Promise((resolve,reject)=>{
  const child=spawn(command,args,{cwd:root,env,stdio:'inherit'});children.push(child);
  child.once('error',reject);child.once('exit',code=>code===0?resolve():reject(new Error(command+' exited: '+code)));
 });
}
function stop(){for(const child of children)if(child.exitCode===null)child.kill('SIGTERM');}
process.on('SIGTERM',()=>{stop();process.exit(143);});process.on('SIGINT',()=>{stop();process.exit(130);});
(async()=>{
 try {
  if(!env.FINOTE_TEST_DATABASE_URL)throw new Error('FINOTE_TEST_DATABASE_URL must point to an isolated test database');
  await fixture('llm-errors','FINOTE_LLM_SIM_URL');
  await fixture('telegram-outbox','FINOTE_TELEGRAM_SIM_URL');
  await fixture('telegram-context','FINOTE_CONTEXT_SIM_URL');
  await fixture('panel-timeout','FINOTE_PANEL_SIM_URL');
  await run('go',['test','-race','-count=1','-timeout','120s','./...']);
  await run('go',['vet','./...']);
  await run(process.execPath,['--test','internal/web/api_test.cjs']);
 } catch(error){console.error(error.message);process.exitCode=1;} finally{stop();}
})();
