// mailflow panel. Every value that comes from Shopify arrives through
// element.innerText (via document.createTextNode), never as markup.
const $ = s => document.querySelector(s);
function h(tag, attrs, ...kids){
  const e = document.createElement(tag);
  for (const [k,v] of Object.entries(attrs||{})) {
    if (k==='class') e.className=v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else if (v!==false && v!=null) e.setAttribute(k,v);
  }
  for (const k of kids.flat()) if (k!=null && k!==false) e.append(k.nodeType?k:document.createTextNode(k));
  return e;
}
function toast(msg){const t=$('#toast');t.textContent=msg;t.style.display='block';clearTimeout(t._t);t._t=setTimeout(()=>t.style.display='none',3200)}
async function api(path, method='GET', body){
  const opt = {method, credentials:'same-origin'};
  if (method==='POST'){ opt.headers={'Content-Type':'application/json'}; opt.body=JSON.stringify(body||{}); }
  const r = await fetch(path,opt);
  if (r.status===401){ location.href='/login?next='+encodeURIComponent(location.pathname); throw new Error('sessão expirada'); }
  const j = await r.json().catch(()=>({}));
  if(!r.ok) throw new Error(j.error||r.statusText);
  return j;
}
const act = (fn, ok) => async()=>{ try{ const r=await fn(); if(ok) toast(typeof ok==='function'?ok(r):ok); await load(); }catch(e){ toast('Erro: '+e.message) } };
const badgeText = {on:'ligado',off:'pausado',draft:'falta escrever'};
const fmt = t => t ? new Date(t).toLocaleString('pt-BR',{dateStyle:'short',timeStyle:'short'}) : '';

function stepCard(f, s){
  const c = s.counts||{};
  return h('div',{class:'step '+(s.effective==='on'?'':s.effective)},
    h('div',{class:'t'},h('span',{class:'num'},s.id),s.title),
    h('div',{class:'timing'},s.timing),
    h('div',{class:'note'},s.note),
    h('span',{class:'badge '+(s.effective==='on'?'':s.effective)},badgeText[s.effective]),
    h('div',{class:'stats'},
      h('span',{},h('b',{},c.sent||0),' enviados'),
      h('span',{},h('b',{},c.pending||0),' na fila'),
      c.skipped?h('span',{},h('b',{},c.skipped),' pulados'):null,
      c.failed?h('span',{class:'f'},h('b',{},c.failed),' falhas'):null),
    h('div',{class:'acts'},
      h('button',{disabled:s.effective==='draft',onclick:()=>preview(f.store.id,s)},'Ver'),
      h('button',{disabled:s.effective==='draft',onclick:()=>{const to=prompt('Enviar teste para qual email?');if(to)act(()=>api(`/api/flows/${f.store.id}/steps/${s.id}/test`,'POST',{to}),r=>'Enviado via '+r.via)()}},'Teste'),
      h('button',{disabled:s.effective==='draft',onclick:act(()=>api(`/api/flows/${f.store.id}/steps/${s.id}/status`,'POST',{status:s.effective==='on'?'off':'on'}))},s.effective==='on'?'Pausar':'Ligar')));
}
function preview(store, s){
  $('#dlgTitle').textContent = `Passo ${s.id} — ${s.title}`;
  fetch(`/api/flows/${store}/steps/${s.id}/preview`).then(async r=>{
    const t = await r.text(); if(!r.ok) throw new Error(JSON.parse(t).error);
    $('#frame').srcdoc = t; $('#dlg').showModal();
  }).catch(e=>toast('Erro: '+e.message));
}
$('#dlgClose').onclick=()=>$('#dlg').close();

function orderRow(o){
  const evSel = h('select',{},...['in_transit','arrived_country','out_for_delivery','ready_for_pickup','delivered'].map(e=>h('option',{value:e},e)));
  return h('tr',{},
    h('td',{},h('b',{},o.name||o.external_id),h('div',{class:'mut'},fmt(o.paid_at)),o.canceled?h('div',{class:'chip failed'},'cancelado'):null),
    h('td',{},o.customer_name,h('div',{class:'mut'},o.email),h('div',{class:'mut'},(o.items||[]).map(i=>`${i.qty}× ${i.title}`).join(', '),' · ',o.total)),
    h('td',{},o.tracking_number?h('span',{},o.tracking_number,h('div',{class:'mut'},o.carrier)):h('span',{class:'mut'},'sem rastreio')),
    h('td',{},(o.jobs||[]).map(j=>h('span',{class:'chip '+j.status+(j.held?' held':''),
        title:`${j.status}${j.held?' (esperando rastreio real)':''} · prevista ${fmt(j.due_at)}${j.reason?' · '+j.reason:''}`,
        onclick:j.status==='failed'||j.status==='skipped'?()=>confirm('Reenfileirar o passo '+j.step_id+'?')&&act(()=>api(`/api/jobs/${j.id}/retry`,'POST'),'Reenfileirado')():null},j.step_id)),
      h('div',{class:'mut'},(o.events||[]).map(e=>e.event).join(' → '))),
    h('td',{},h('div',{class:'acts'},
      h('button',{onclick:()=>{const n=prompt('Código de rastreio real:');if(n)act(()=>api(`/api/orders/${o.id}/tracking`,'POST',{number:n,carrier:prompt('Transportadora (opcional):')||''}),'Rastreio salvo')()}},'Rastreio'),
      evSel,h('button',{onclick:act(()=>api(`/api/orders/${o.id}/events`,'POST',{event:evSel.value}),'Evento registrado')},'Marcar'),
      h('button',{disabled:o.canceled,onclick:()=>confirm('Cancelar todos os emails pendentes?')&&act(()=>api(`/api/orders/${o.id}/cancel`,'POST'),'Cancelado')()},'Cancelar'))));
}
async function load(){
  const flows = await api('/api/flows');
  const orders = await api('/api/orders');
  $('#sender').textContent = flows[0] ? 'envio: '+flows[0].sender : '';
  $('#sender').className = 'pill'+(flows[0]&&flows[0].sender.startsWith('log')?' warn':'');
  $('#clock').textContent = 'atualizado '+new Date().toLocaleTimeString('pt-BR');
  const main = $('#main'); main.replaceChildren();
  if (!flows.length) { main.append(h('p',{class:'mut'},'Nenhuma loja vinculada à sua conta. Peça um convite ao dono da loja.')); return; }
  for (const f of flows){
    const row = h('div',{class:'row'});
    f.steps.forEach((s,i)=>{ if(i) row.append(h('div',{class:'arrow'})); row.append(stepCard(f,s)); });
    main.append(h('div',{class:'lane'},h('h2',{},f.store.name,h('small',{},f.store.from)),row));
    const mine = orders.filter(o=>o.store_id===f.store.id);
    const name=h('input',{placeholder:'nome do cliente'}), email=h('input',{placeholder:'email do cliente',size:26});
    main.append(h('h3',{},'Pedidos — '+f.store.name),
      h('div',{class:'form'},name,email,h('button',{onclick:act(()=>api('/api/orders','POST',{store:f.store.id,customer:name.value,email:email.value}),'Pedido de teste criado')},'Novo pedido de teste')),
      mine.length?h('table',{},h('thead',{},h('tr',{},['Pedido','Cliente','Rastreio','Jornada (passos)','Ações'].map(t=>h('th',{},t)))),h('tbody',{},mine.map(orderRow))):h('div',{class:'mut'},'Nenhum pedido ainda.'));
  }
}
load().catch(e=>toast('Erro: '+e.message));
setInterval(()=>{ if(!document.querySelector('dialog[open]')&&!document.activeElement.matches('input,select')) load().catch(()=>{}) },20000);
