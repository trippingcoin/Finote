const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function client(fetcher = fetch) {
  const context = { fetch: fetcher, AbortController, setTimeout, clearTimeout };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'static/api.js'), 'utf8'), context);
  return context.api;
}

test('client timeout, response body timeout, unknown writes and explicit cancel over HTTP', async t => {
  let requests = 0;
  const server = http.createServer((req, res) => {
    requests++;
    if (req.url === '/slow') {
      const timer = setTimeout(() => res.end('{}'), 500);
      res.on('close', () => clearTimeout(timer)); return;
    }
    if (req.url === '/body') {
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.write('{');
      const timer = setTimeout(() => res.end('}'), 500);
      res.on('close', () => clearTimeout(timer)); return;
    }
    if (req.url === '/disconnect') { req.socket.destroy(); return; }
    if (req.url === '/503') { res.writeHead(503); res.end('{"error":"timeout","code":"request_timeout"}'); return; }
    if (req.url === '/400') { res.writeHead(400); res.end('{"error":"validation"}'); return; }
    if (req.url === '/invalid') { res.end('<html>bad response</html>'); return; }
    if (req.url === '/204') { res.writeHead(204); res.end(); return; }
    res.end('{"id":"test-id"}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); return new Promise(resolve => server.close(resolve)); });
  const base = 'http://127.0.0.1:' + server.address().port;
  const api = client();
  for (const route of ['/slow', '/body']) {
    await assert.rejects(api(base + route, {}, 80), e => e.name !== 'AbortError' && e.unknownResult === false && /вовремя/.test(e.message));
  }
  for (const route of ['/slow', '/body', '/503', '/invalid', '/disconnect']) {
    const before = requests;
    await assert.rejects(api(base + route, { method: 'POST', body: '{}' }, 80),
      e => e.unknownResult === true && /Проверьте список/.test(e.message));
    assert.equal(requests - before, 1, 'write must never retry automatically');
  }
  await assert.rejects(api(base + '/400', { method: 'POST' }), e => e.unknownResult === false && e.message === 'validation');
  const controller = new AbortController();
  const pending = api(base + '/slow', { signal: controller.signal }, 1000);
  setTimeout(() => controller.abort(), 30);
  await assert.rejects(pending, e => e.name === 'AbortError');
  assert.equal((await api(base + '/ok', { method: 'POST' })).id, 'test-id');
  assert.equal(await api(base + '/204', { method: 'DELETE' }), null);
});

test('production client against local Postman fault simulation', {
  skip: !process.env.FINOTE_PANEL_SIM_URL
}, async () => {
  const api = client(), base = process.env.FINOTE_PANEL_SIM_URL;
  await assert.rejects(api(base + '/slow', { method: 'POST' }, 80), e => e.unknownResult === true);
  await assert.rejects(api(base + '/error', { method: 'POST' }), e => e.unknownResult === true);
});

test('PATCH contains only actual changes; missing comment is equivalent to empty', () => {
  const context = {};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'static/api.js'), 'utf8'), context);
  const original = { amount:400,category:'еда',spent_at:'2026-10-22T10:00:00Z',version:1 };
  const values = { ...original,comment:'' };
  assert.equal(JSON.stringify(context.paymentChanges(original,values)), '{}');
  values.comment = 'с собой';
  assert.equal(JSON.stringify(context.paymentChanges(original,values)), '{"comment":"с собой"}');
});

test('409 is a known conflict, not an unknown save outcome', async () => {
  const api = client(async () => ({status:409,ok:false,json:async()=>({error:'Обновите запись',code:'version_conflict'})}));
  await assert.rejects(api('/v1/payments/id',{method:'PATCH',body:'{}'}), error =>
    error.code==='version_conflict' && error.status===409 && error.unknownResult===false);
});

test('conflict keeps input, prevents retries, reloads explicitly; names are text', async () => {
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) elements.set(id, {
      value:'',hidden:false,disabled:false,handlers:{},children:[],
      addEventListener(name,handler){this.handlers[name]=handler;},
      append(...children){this.children.push(...children);},replaceChildren(...children){this.children=children;},
      setAttribute(){},focus(){},reset(){},showModal(){this.open=true;},close(){this.open=false;},reportValidity(){return true;}
    });
    return elements.get(id);
  }
  const original = {id:'test',user_id:'10',amount:400,category:'еда',comment:'кофе',spent_at:'2026-10-22T10:00:00Z',version:1};
  const latest = {...original,amount:500,version:2};
  const writes=[];
  const context=vm.createContext({
    document:{getElementById:element,createElement:()=>element(Symbol()),createTextNode:text=>({textContent:text})},
    Intl,AbortController,URLSearchParams,FormData:class{*[Symbol.iterator](){}},setTimeout:()=>0,clearTimeout(){},
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname,'static/api.js'),'utf8'),context);
  context.api=async (url,options={})=>{
    if(options.method){writes.push(JSON.parse(options.body));const error=new Error('Конфликт');error.code='version_conflict';error.unknownResult=false;throw error;}
    if(url.startsWith('/v1/users'))return [{id:'10',display_name:'<img src=x onerror=alert(1)>',username:'name'}];
    if(url==='/v1/payments/test')return latest;
    return {items:[original],total:1,members:1,amount:'400'};
  };
  vm.runInContext(fs.readFileSync(path.join(__dirname,'static/app.js'),'utf8'),context);
  await new Promise(resolve=>setImmediate(resolve));
  assert.match(element('filter-user').children[1].textContent, /<img src=x/);
  assert.equal(element('filter-user').children[1].innerHTML,undefined,'profile must not become HTML');
  context.openEditor(original);element('payment-comment').value='с собой';
  const submit=()=>element('payment-form').handlers.submit({preventDefault(){}});
  await submit();
  assert.deepEqual(writes,[{comment:'с собой',expected_version:1}]);
  assert.equal(element('payment-comment').value,'с собой');
  assert.equal(element('save').disabled,true);assert.equal(element('reload-payment').hidden,false);
  await submit();assert.equal(writes.length,1);
  await element('reload-payment').handlers.click();
  assert.equal(element('payment-amount').value,500);assert.equal(element('save').disabled,false);
  assert.equal(vm.runInContext('state.editing.version',context),2);
  await submit();assert.equal(writes.length,1,'unchanged form must not PATCH');
});

test('forms unlock on unknown outcome, prevent resubmit and offer read-only verification', async () => {
  const elements = new Map();
  function element(id) {
    if (!elements.has(id)) elements.set(id, {
      value: '', hidden: false, disabled: false, handlers: {},
      addEventListener(name, handler) { this.handlers[name] = handler; },
      append() {}, replaceChildren() {}, setAttribute() {}, focus() {}, reset() {},
      showModal() { this.open = true; }, close() { this.open = false; },
      reportValidity() { return true; }
    });
    return elements.get(id);
  }
  let writes = 0;
  const context = vm.createContext({
    document: { getElementById: element, createElement: () => element(Symbol()), createTextNode: x => x },
    Intl, AbortController, URLSearchParams, FormData: class { *[Symbol.iterator]() {} },
    setTimeout, clearTimeout,
    api: async (url, options = {}) => {
      if (options.method) {
        writes++;
        const err = new Error('Проверьте список расходов'); err.unknownResult = true; throw err;
      }
      return url.startsWith('/v1/users') ? [] : { items: [], total: 0, members: 0, amount: '0' };
    }
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/app.js'), 'utf8'), context);
  await new Promise(resolve => setImmediate(resolve));
  context.openEditor();
  element('payment-amount').value = '400';
  element('payment-date').value = '2026-10-22';
  element('payment-category').value = 'еда';
  element('payment-user').value = '123';
  const submit = () => element('payment-form').handlers.submit({ preventDefault() {} });
  await submit();
  assert.equal(element('save').disabled, true);
  assert.equal(element('check-save').hidden, false);
  assert.equal(vm.runInContext('state.saving', context), false);
  await submit(); assert.equal(writes, 1);
  context.closeEditor(); assert.equal(element('editor').open, false);
  element('check-save').handlers.click();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(writes, 1, 'checking list must not repeat POST');
  context.openDelete({ id: 'test-id', amount: 400, category: 'еда' });
  await element('confirm-delete').handlers.click();
  assert.equal(element('confirm-delete').disabled, true);
  assert.equal(element('check-delete').hidden, false);
  assert.equal(vm.runInContext('state.removing', context), false);
  await element('confirm-delete').handlers.click(); assert.equal(writes, 2);
  context.closeDelete(); assert.equal(element('delete-dialog').open, false);
});
