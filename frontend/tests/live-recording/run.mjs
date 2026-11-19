import {chromium,expect} from '@playwright/test';
import {readFile,mkdir} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
const output=new URL('../../../output/playwright/live-recording/',import.meta.url);
await mkdir(output,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true});
const checks=[];let page;
try{
 page=await browser.newPage({viewport:{width:1440,height:1000},timezoneId:'Asia/Tokyo'});page.setDefaultTimeout(10000);
 const errors=[];page.on('pageerror',error=>{errors.push(error.message);console.error(error.message);});
 await page.clock.setFixedTime(new Date('2026-11-17T18:00:00Z'));
 await page.route('http://127.0.0.1:13131/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html lang="pt-BR" class="dark" style="--font-inter:Arial,sans-serif"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/fixture.css"><style>body{margin:0}.astra-interface{padding:12px}.live-workspace{height:86vh}.fixture-controls{display:flex;gap:12px;margin:8px 0}</style></head><body><div id="root"></div><script src="/fixture.js"></script></body></html>'});
  if(path==='/fixture.js'||path==='/fixture.css')return route.fulfill({contentType:path.endsWith('.css')?'text/css':'text/javascript',body:await readFile(new URL('.generated'+path,import.meta.url))});
  if(path.startsWith('/fixture-media/')){const name=path.split('/').at(-1);if(!/^(manifest\.m3u8|init\.mp4|part\d+\.m4s)$/.test(name))return route.abort();return route.fulfill({contentType:name.endsWith('m3u8')?'application/vnd.apple.mpegurl':'video/mp4',body:await readFile(new URL('.generated/media/'+name,import.meta.url))});}
  return route.abort();
 });
 const panel=()=>page.getByRole('region',{name:'Gravações da câmera'});
 const posts=()=>page.evaluate(()=>window.recordingQA.requests.filter(r=>r.method==='POST'&&r.path.endsWith('/playback-sessions')).length);
 const fresh=async()=>{await page.goto('http://127.0.0.1:13131/');await page.getByRole('button',{name:'Mosaico repetido',exact:true}).click();await expect(page.locator('[data-live-position="0"] video')).toBeVisible();};
 const open=async(index=0)=>{await page.locator(`[data-live-position="${index}"]`).click({button:'right'});await page.getByRole('menuitem',{name:'Gravação',exact:true}).click();await expect(panel().getByRole('group',{name:'Linha do tempo de gravações',exact:true})).toBeVisible();await expect(panel().getByText('Consultando gravações…',{exact:true})).toHaveCount(0);};
 const go=async(time)=>{
  await panel().getByRole('button',{name:'Escolher data das gravações'}).click();
  await page.getByRole('button',{name:'17/11/2026, com gravações',exact:true}).click();
  const ruler=panel().getByRole('group',{name:'Linha do tempo de gravações',exact:true});
  const target=Date.parse('2026-11-17T'+time+'-03:00')+250;
  let span=86400000,center=Date.parse('2026-11-17T12:00:00-03:00');
  while(true){
   const steps=Math.round((target-center)/(span*.1));
   for(let i=0;i<Math.abs(steps);i++)await ruler.press(steps<0?'ArrowLeft':'ArrowRight');
   center+=steps*span*.1;
   if(span===60000)break;
   await ruler.press('+');span=Math.max(60000,span/2);
  }
  const box=await ruler.boundingBox();
  await page.mouse.click(box.x+box.width*((target-center)/span+.5),box.y+42);
 };
 const setSpeed=async(value)=>{
  const rates=[-8,-4,-2,-1,-.5,.5,1,2,4,8];
  const output=panel().getByLabel('Velocidade da gravação',{exact:true});
  let current=Number((await output.innerText()).replace('×','').replace(',','.'));
  while(current!==value){
   const direction=rates.indexOf(value)>rates.indexOf(current)?1:-1;
   await panel().getByRole('button',{name:direction>0?'Aumentar velocidade da gravação':'Diminuir velocidade da gravação',exact:true}).click();
   current=rates[rates.indexOf(current)+direction];
  }
 };
 const video=()=>page.locator('.recording-video video');
 // Keep the real pointer gesture active while an archive notification arrives.
 await fresh();await open();
 const qaRuler=panel().getByRole('group',{name:'Linha do tempo de gravações',exact:true});
 await qaRuler.press('+');
 await expect.poll(()=>panel().locator('.recording-available').count()).toBeGreaterThan(0);
 const qaCalls=()=>page.evaluate(()=>window.recordingQA.requests.filter(r=>r.path.includes('recording-availability?start=')).length);
 const qaBefore=await qaCalls(),qaBox=await qaRuler.boundingBox();
 const qaWidth=await panel().locator('.recording-available').evaluateAll(nodes=>nodes.reduce((sum,node)=>sum+parseFloat(node.style.width),0));
 await page.mouse.move(qaBox.x+qaBox.width*.5,qaBox.y+42);await page.mouse.down();
 await page.mouse.move(qaBox.x+qaBox.width*.49,qaBox.y+42);
 expect(await panel().locator('.recording-available').count()).toBeGreaterThan(0);
 await page.waitForTimeout(180);expect(await qaCalls()).toBe(qaBefore);
 await page.evaluate(()=>window.recordingQA.grow());
 for(let i=0;i<12;i++){await page.mouse.move(qaBox.x+qaBox.width*(.49-i*.0001),qaBox.y+42);await page.waitForTimeout(20);expect(await panel().locator('.recording-available').count()).toBeGreaterThan(0);}
 await expect.poll(()=>panel().locator('.recording-available').evaluateAll(nodes=>nodes.reduce((sum,node)=>sum+parseFloat(node.style.width),0))).toBeGreaterThan(qaWidth);
 await page.mouse.up();
 const qaAfter=await qaCalls();await page.evaluate(()=>window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'ArchiveChanged',cameraId:'cam2'}})));
 await page.waitForTimeout(220);expect(await qaCalls()).toBe(qaAfter);
 await page.evaluate(()=>window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'Disconnected'}})));
 await expect(panel()).toContainText('Disponibilidade pode estar desatualizada');expect(await panel().locator('.recording-available').count()).toBeGreaterThan(0);
 await page.evaluate(()=>window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'Connected'}})));
 await expect(panel()).not.toContainText('Disponibilidade pode estar desatualizada');
 checks.push('faixa azul preservada sem consulta durante pan conhecido, crescimento SSE durante arraste e filtro por câmera');
 await fresh();await open();
 await expect(panel().locator('.recording-panel-status, .recording-toolbar')).toHaveCount(0);
 await expect(panel().getByRole('button',{name:/Recolher barra|Expandir barra/})).toHaveCount(0);
 const zoomSlider=panel().getByRole('slider',{name:'Zoom da linha do tempo',exact:true});
 await zoomSlider.press('Home');await expect(panel().getByRole('button',{name:'Diminuir zoom da linha do tempo'})).toBeDisabled();
 await zoomSlider.press('End');await expect(panel().getByRole('button',{name:'Aumentar zoom da linha do tempo'})).toBeDisabled();
 await expect(zoomSlider).toHaveAttribute('aria-valuetext','Janela de 1 minuto');
 await panel().getByRole('button',{name:'Diminuir zoom da linha do tempo'}).click();expect(Number(await zoomSlider.inputValue())).toBeLessThan(100);
 const navigation=panel().locator('.recording-navigation-controls');await expect(navigation.getByRole('button',{name:'Fechar painel de gravações'})).toBeVisible();
 checks.push('status e recolhimento removidos; slider sincronizado com zoom e fechamento ao lado dos controles');
 await fresh();await open();
 expect(await posts()).toBe(0);await expect(page.locator('[data-live-position="0"]')).toContainText('Escolha um horário');await expect(page.locator('[data-live-position="1"] video')).toBeVisible();
 await expect(panel().getByLabel('Horário exato da gravação')).toHaveCount(0);await expect(panel().getByRole('button',{name:/Visão de 24h|Acompanhar reprodução|Voltar ao vivo|^Ir$/})).toHaveCount(0);await expect(page.locator('.recording-mode-badge')).toHaveCount(0);await expect(panel().getByRole('button',{name:'Escolher data das gravações'})).toContainText('17:11:2026');checks.push('abertura sem vídeo, quadro isolado e fuso do site independente do navegador');
 await panel().getByRole('button',{name:'Escolher data das gravações'}).click();
 await expect(page.getByRole('button',{name:'17/11/2026, com gravações',exact:true})).toHaveAttribute('data-recorded','true');await expect(page.getByRole('button',{name:'15/11/2026, sem gravações',exact:true})).toHaveAttribute('data-recorded','false');
 await expect(page.getByText('Dias em azul possuem gravações',{exact:true})).toHaveCount(0);expect((await page.locator('.recording-calendar').boundingBox()).width).toBeLessThanOrEqual(224);await page.screenshot({path:fileURLToPath(new URL('calendar-dark.png',output)),fullPage:true});await page.keyboard.press('Escape');
 await page.evaluate(()=>window.recordingQA.failMonth(true));await panel().getByRole('button',{name:'Escolher data das gravações'}).click();await expect(page.locator('.recording-calendar-status[role="alert"]')).toBeVisible();await expect(page.getByRole('button',{name:'17/11/2026, disponibilidade desconhecida',exact:true})).toBeVisible();await page.evaluate(()=>window.recordingQA.failMonth(false));await page.locator('.recording-calendar').getByRole('button',{name:'Tentar novamente',exact:true}).click();await expect(page.getByRole('button',{name:'17/11/2026, com gravações',exact:true})).toBeVisible();await page.keyboard.press('Escape');checks.push('calendário azul por disponibilidade e erro distinto de dia vazio');
 await panel().getByRole('button',{name:'Diminuir zoom da linha do tempo'}).click();await expect(panel().getByRole('button',{name:'Diminuir zoom da linha do tempo'})).toBeDisabled();
 const ruler=panel().getByRole('group',{name:'Linha do tempo de gravações',exact:true});let box=await ruler.boundingBox();await page.mouse.move(box.x+box.width*.55,box.y+42);await expect(panel().locator('.recording-hover-time')).toContainText(/\d\d:\d\d:\d\d/);
 const before=await posts();await page.mouse.down();await page.mouse.move(box.x+box.width*.25,box.y+42,{steps:8});await page.mouse.up();expect(await posts()).toBe(before);
 await page.mouse.move(box.x+box.width*.5,box.y+42);await page.mouse.wheel(0,-200);await expect(panel().getByRole('button',{name:'Diminuir zoom da linha do tempo'})).toBeEnabled();
 for(let i=0;i<12;i++)await panel().getByRole('button',{name:'Aumentar zoom da linha do tempo'}).click({force:true});await expect(panel().getByRole('button',{name:'Aumentar zoom da linha do tempo'})).toBeDisabled();
 await panel().getByRole('button',{name:'Escolher data das gravações'}).click();await page.getByRole('button',{name:'17/11/2026, com gravações',exact:true}).click();
 const divider=panel().getByRole('separator',{name:'Largura do nome da câmera'});const card=panel().getByRole('button',{name:'Escolher data das gravações',exact:true});const cardWidth=(await card.boundingBox()).width;await divider.focus();await page.keyboard.press('ArrowRight');expect(Number(await divider.getAttribute('aria-valuenow'))).toBeGreaterThan(190);expect((await card.boundingBox()).width).toBeGreaterThan(cardWidth);expect(Math.abs((await card.boundingBox()).width-(await panel().locator('.recording-camera-name').boundingBox()).width)).toBeLessThan(1);checks.push('arraste independente, zoom de 1 minuto a 48h, tooltip e divisor por teclado');
 await go('09:00:00');await expect.poll(()=>video().evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);await expect.poll(()=>video().evaluate(v=>v.currentTime)).toBeGreaterThan(.1);
 await expect(page.locator('[data-live-position="1"] video')).toBeVisible();await setSpeed(2);await expect.poll(()=>video().evaluate(v=>v.playbackRate)).toBe(2);
 await panel().getByRole('button',{name:'Pausar gravação'}).click();await expect.poll(()=>video().evaluate(v=>v.paused)).toBe(true);await go('09:00:10');await expect.poll(()=>video().evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);expect(await video().evaluate(v=>v.paused)).toBe(true);
 await page.screenshot({path:fileURLToPath(new URL('playback-paused-dark.png',output)),fullPage:true});checks.push('vídeo HLS decodificado, velocidade real e pausa preservada ao buscar');
 const renewals=()=>page.evaluate(()=>window.recordingQA.requests.filter(r=>r.method==='PUT'&&r.path.includes('/playback-sessions/')).length);
 const renewalBefore=await renewals();await page.waitForTimeout(4200);const renewalCount=(await renewals())-renewalBefore;
 expect(renewalCount).toBeGreaterThanOrEqual(1);expect(renewalCount).toBeLessThanOrEqual(3);checks.push('sessão pausada continua renovada com frequência de dois segundos');
 // The pointer commit, not pointermove, owns the new playback request.
 const marker=panel().getByRole('slider',{name:'Posição de reprodução'});box=await marker.boundingBox();const count=await posts();await page.mouse.move(box.x+7,box.y+10);await page.mouse.down();await page.mouse.move(box.x+24,box.y+10,{steps:5});expect(await posts()).toBe(count);await page.mouse.up();await expect.poll(posts).toBe(count+1);checks.push('arrastar marcador busca somente ao soltar');
 await go('09:00:00');await setSpeed(8);await panel().getByRole('button',{name:'Reproduzir gravação'}).click();
 await expect(page.locator('.recording-video-message')).toContainText('Sem gravação neste período.',{timeout:20000});await expect(panel().getByLabel('Horário da reprodução')).toContainText('09:00:36');expect(await video().evaluate(v=>v.paused)).toBe(true);await expect.poll(()=>page.evaluate(()=>window.recordingQA.sessions.size)).toBe(0);checks.push('continuidade automática entre sessões e parada na lacuna real');
 await page.getByRole('button',{name:'Video Wall',exact:true}).click();await expect(panel()).toBeVisible();expect(await page.evaluate(()=>!!document.fullscreenElement)).toBe(true);await page.screenshot({path:fileURLToPath(new URL('videowall-dark.png',output)),fullPage:true});
 await expect(panel().getByRole('group',{name:'Controles de reprodução de gravações'})).toBeVisible();await expect(ruler).toBeVisible();await expect(zoomSlider).toBeVisible();await page.keyboard.press('Escape');await expect.poll(()=>page.evaluate(()=>!!document.fullscreenElement)).toBe(false);checks.push('tela cheia com linha do tempo e controles acessíveis');
 await panel().getByRole('button',{name:'Fechar painel de gravações'}).click();await expect(panel()).toHaveCount(0);await expect(page.locator('[data-live-position="0"] video')).toBeVisible();await open(1);await page.getByRole('button',{name:'Layout 1',exact:true}).click();await expect(panel()).toHaveCount(0);checks.push('fechar restaura ao vivo e retirar quadro do layout encerra a barra');
 await fresh();await open();await page.evaluate(()=>window.recordingQA.hold(true));await go('09:00:00');await expect.poll(posts).toBe(1);await panel().getByRole('button',{name:'Fechar painel de gravações'}).click();await page.evaluate(()=>window.recordingQA.release());await expect.poll(()=>page.evaluate(()=>window.recordingQA.sessions.size)).toBe(0);checks.push('resposta atrasada após fechamento libera sessão');
 await open();await go('09:00:00');await expect.poll(()=>video().evaluate(v=>v.readyState)).toBeGreaterThanOrEqual(2);await page.evaluate(()=>{window.recordingQA.deny(true);window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'AuthorizationChanged'}}));});await expect.poll(()=>page.evaluate(()=>window.recordingQA.sessions.size)).toBe(0);await expect(panel().locator('.recording-toast[role="alert"]')).toBeVisible();
 await page.evaluate(()=>window.recordingQA.deny(false));await panel().locator('.recording-toast').getByRole('button',{name:'Tentar novamente',exact:true}).click();await expect(panel().locator('.recording-toast')).toHaveCount(0);await expect.poll(()=>panel().locator('.recording-available').count()).toBeGreaterThan(0);checks.push('revogação remove vídeo e disponibilidade; erro aparece em toast e consulta se recupera ao tentar novamente');
 await fresh();await open();await go('08:00:00');await expect(page.locator('.recording-video-message')).toContainText('Sem gravação');await go('09:00:00');await expect.poll(()=>video().evaluate(v=>v.readyState>=2&&!v.paused)).toBe(true);await panel().getByRole('button',{name:'Pausar gravação'}).click();
 await panel().getByRole('button',{name:'Próxima gravação',exact:true}).click();await expect(panel().getByLabel('Horário da reprodução')).toContainText('13:00:00');await expect.poll(()=>video().evaluate(v=>v.readyState>=2&&v.paused)).toBe(true);
 const currentPosts=await posts();await page.evaluate(()=>{window.qaVideo=document.querySelector('.recording-video video');});await panel().getByRole('button',{name:'Escolher data das gravações'}).click();await page.getByRole('button',{name:'16/11/2026, com gravações',exact:true}).click();expect(await posts()).toBe(currentPosts);expect(await page.evaluate(()=>window.qaVideo===document.querySelector('.recording-video video'))).toBe(true);await expect(panel().getByLabel('Horário da reprodução')).toContainText('17:11:2026');checks.push('primeiro trecho após horário vazio inicia vídeo; próximo preserva pausa; calendário preserva transporte');
 await fresh();await open();await go('23:59:50');await setSpeed(8);await expect(page.locator('.recording-video-message')).toContainText('Fim da gravação disponível.',{timeout:15000});await expect(panel().getByLabel('Horário da reprodução')).toContainText('00:00:14');expect(await posts()).toBe(2);checks.push('continuidade e relógio original através da meia-noite');
 await fresh();await open();await go('09:00:25');await expect.poll(()=>video().evaluate(v=>v.readyState>=2&&!v.paused)).toBe(true);
 await panel().getByRole('button',{name:'Pausar gravação'}).click();await setSpeed(-2);
 await expect(panel().getByRole('combobox',{name:'Velocidade da gravação'})).toHaveCount(0);
 expect(await video().evaluate(v=>v.playbackRate)).toBe(2);
 const reversePosts=await posts();await panel().getByRole('button',{name:'Reproduzir gravação'}).click();
 await expect.poll(async()=>Number(await panel().getByRole('slider',{name:'Posição de reprodução'}).getAttribute('aria-valuenow')),{timeout:15000}).toBeLessThan(Date.parse('2026-11-17T12:00:12Z'));
 expect(await posts()).toBeGreaterThan(reversePosts);await panel().getByRole('button',{name:'Pausar gravação'}).click();
 const pausedAt=Number(await panel().getByRole('slider',{name:'Posição de reprodução'}).getAttribute('aria-valuenow'));
 await page.waitForTimeout(350);expect(Math.abs(Number(await panel().getByRole('slider',{name:'Posição de reprodução'}).getAttribute('aria-valuenow'))-pausedAt)).toBeLessThan(300);
 await setSpeed(1);await panel().getByRole('button',{name:'Reproduzir gravação'}).click();await expect.poll(()=>video().evaluate(v=>!v.paused&&v.playbackRate===1)).toBe(true);
 await setSpeed(-8);await expect(panel().getByRole('button',{name:'Diminuir velocidade da gravação'})).toBeDisabled();
 await expect(page.locator('.recording-video-message')).toContainText('Sem gravação anterior contínua.',{timeout:15000});
 await expect.poll(()=>page.evaluate(()=>window.recordingQA.sessions.size)).toBe(0);checks.push('setas de velocidade, retrocesso entre sessões, pausa, retorno ao avanço e parada na lacuna anterior');
 await fresh();await open();await go('23:59:55');await setSpeed(-1);
 const midnightBefore=Number(await panel().getByRole('slider',{name:'Posição de reprodução'}).getAttribute('aria-valuenow'));
 await expect.poll(async()=>Number(await panel().getByRole('slider',{name:'Posição de reprodução'}).getAttribute('aria-valuenow'))).toBeLessThan(midnightBefore-500);
 await panel().getByRole('button',{name:'Fechar painel de gravações'}).click();await expect.poll(()=>page.evaluate(()=>window.recordingQA.sessions.size)).toBe(0);
 checks.push('fechar durante retrocesso libera a sessão');
 for(const dark of [true,false])for(const width of [1440,390,320]){
  await page.setViewportSize({width,height:1000});await fresh();await page.evaluate(d=>document.documentElement.classList.toggle('dark',d),dark);await open();
  expect(await panel().evaluate(e=>e.getBoundingClientRect().right<=innerWidth+1&&e.scrollWidth<=e.clientWidth+1)).toBe(true);
  expect(await panel().evaluate(e=>[...e.querySelectorAll('button:not(:disabled)')].every(button=>{const b=button.getBoundingClientRect();const p=e.getBoundingClientRect();if(b.top<p.top||b.bottom>p.bottom)return true;const hit=document.elementFromPoint(b.x+b.width/2,b.y+b.height/2);return button===hit||button.contains(hit);}))).toBe(true);
  await page.screenshot({path:fileURLToPath(new URL(`timeline-${dark?'dark':'light'}-${width}.png`,output)),fullPage:true});
 }
 checks.push('layout em 1440, 390 e 320px nos dois temas');expect(errors).toEqual([]);console.log(JSON.stringify({checks},null,2));
}catch(error){console.error(error);if(page){console.log(await page.locator('body').innerText());await page.screenshot({path:fileURLToPath(new URL('failure.png',output)),fullPage:true,timeout:5000}).catch(()=>{});}throw error;}finally{await browser.close();}
