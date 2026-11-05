async page => {
 const checks=[];
 const assert=(condition,message)=>{if(!condition)throw new Error(message);};
 const dialog=()=>page.getByRole('dialog');
 const panel=name=>dialog().getByRole('tabpanel',{name,exact:true});
 const info=()=>panel('Informações');
 const streams=()=>panel('Streams');
 const section=async name=>{await dialog().getByRole('tab',{name,exact:true}).click();};
 const fresh=async()=>{await page.goto('http://127.0.0.1:13119');await page.getByRole('button',{name:'+ Nova câmera',exact:true}).click();};
 const requests=()=>page.evaluate(()=>window.cameraFormQA.requests);
 const lastMutation=async()=>[...(await requests())].reverse().find(r=>r.method==='PATCH'||r.method==='POST'&&r.path.endsWith('/cameras'));
 const save=async()=>{await dialog().getByRole('button',{name:/^Salvar/}).click();};
 const close=async()=>{await dialog().getByRole('button',{name:'Cancelar',exact:true}).click();await dialog().waitFor({state:'detached'});};
 const edit=async name=>{await page.getByRole('row').filter({hasText:name}).getByRole('button',{name:'Editar',exact:true}).click();};
 const layoutFits=async()=>dialog().evaluate(el=>{
  const bounds=el.getBoundingClientRect();
  return bounds.left>=0&&bounds.right<=innerWidth+1&&bounds.top>=0&&bounds.bottom<=innerHeight+1&&el.scrollWidth<=el.clientWidth+1;
 });

 try {
 await page.setViewportSize({width:1280,height:900});
 await fresh();
 assert(await info().getByRole('textbox',{name:'Endereço RTSP principal',exact:true}).isVisible(),'Main RTSP connection must be in Informações');
 assert(await dialog().getByRole('tab').count()===2,'Creation must have two sections');
 assert(await info().getByRole('combobox',{name:'Tipo de conexão'}).locator('option').count()===6,'Connection picker lost options');
 for(const name of ['ONVIF','Axis','Intelbras']){
  assert(await info().getByRole('combobox',{name:'Tipo de conexão'}).locator('option').filter({hasText:name}).evaluate(el=>el.disabled),'Unimplemented integration must stay disabled: '+name);
 }
 assert(await info().getByRole('combobox',{name:'Site',exact:true}).locator('option').count()===3,'Unauthorized site leaked into picker');
 await info().getByRole('textbox',{name:'Nome',exact:true}).fill('Entrada de teste');
 await info().getByRole('combobox',{name:'Site',exact:true}).selectOption('site');
 await section('Streams');
 await save();
 assert(await info().isVisible(),'Missing main URL did not open Informações');
 assert(await info().getByRole('textbox',{name:'Endereço RTSP principal'}).evaluate(el=>el===document.activeElement),'Invalid URL field was not focused');
 assert(!(await requests()).some(r=>r.method==='POST'),'Invalid form triggered a mutation');
 await info().getByRole('textbox',{name:'Endereço RTSP principal'}).fill('rtsp://192.0.2.40/main');
 await info().getByRole('button',{name:'Testar conexão',exact:true}).click();
 await info().getByText(/Conexão estabelecida/).waitFor();
 await section('Streams');
 assert(await streams().getByRole('textbox',{name:'Endereço RTSP principal'}).count()===0,'Main URL is duplicated in Streams');
 await streams().getByRole('button',{name:'+ Adicionar substream',exact:true}).click();
 await section('Informações');
 await save();
 assert(await streams().isVisible(),'Missing substream URL did not open Streams');
 await streams().getByRole('textbox',{name:'Endereço RTSP · Substream 1',exact:true}).fill('rtsp://192.0.2.40/sub1');
 await streams().getByRole('button',{name:'Testar conexão',exact:true}).click();
 await streams().getByText(/Conexão estabelecida/).waitFor();
 await section('Informações');
 await info().getByText(/Conexão estabelecida/).waitFor();
 assert(await info().getByRole('textbox',{name:'Nome',exact:true}).inputValue()==='Entrada de teste','Switching sections lost form state');
 await save();await dialog().waitFor({state:'detached'});
 const created=await lastMutation();
 assert(created.body.profiles.length===2&&created.body.profiles[0].url==='rtsp://192.0.2.40/main'&&created.body.profiles[1].url==='rtsp://192.0.2.40/sub1','Create payload lost RTSP profiles');
 checks.push('RTSP creation, main test in Informações, substream test in Streams, section state and hidden-field validation');

 await fresh();await section('Streams');
 for(let i=1;i<=4;i++){await streams().getByRole('button',{name:'+ Adicionar substream',exact:true}).click();}
 assert(await streams().getByRole('button',{name:'+ Adicionar substream',exact:true}).count()===0,'Substream limit changed');
 await streams().getByRole('group',{name:'Substream 2',exact:true}).getByRole('button',{name:'Remover substream'}).click();
 await streams().getByRole('button',{name:'+ Adicionar substream'}).click();
 assert(await streams().getByRole('group',{name:'Substream 2',exact:true}).count()===1,'Removed substream slot was not reused');
 await close();
 checks.push('Four optional RTSP substreams, removal and slot reuse');

 await edit('RTSP existente');
 await section('Streams');
 await streams().getByRole('group',{name:'Substream 1',exact:true}).getByRole('button',{name:'Remover substream'}).click();
 await section('Informações');await save();await dialog().waitFor({state:'detached'});
 let updated=await lastMutation();
 assert(updated.body.revision===7&&updated.body.source.profiles.length===1&&updated.body.source.profiles[0].url==='','RTSP edit did not preserve the existing main address');
 checks.push('RTSP edit preserves saved URL while removing a substream');

 await edit('Dahua existente');
 assert(await info().getByLabel('Senha',{exact:true}).inputValue()==='*********','Saved password is not masked');
 await info().getByRole('textbox',{name:'Nome',exact:true}).fill('Dahua renomeada');
 await save();await dialog().waitFor({state:'detached'});
 updated=await lastMutation();assert(!('source' in updated.body),'Renaming rewrote saved source credentials');
 await edit('Dahua renomeada');
 await info().getByRole('textbox',{name:'Endereço',exact:true}).fill('192.0.2.41');
 await save();await dialog().waitFor({state:'detached'});
 updated=await lastMutation();assert(updated.body.source.connection.host==='192.0.2.41'&&!('password' in updated.body.source.connection),'Saved password mask was sent to the backend');
 checks.push('Dahua edit preserves password and sends no source when only renaming');

 await fresh();
 await info().getByRole('textbox',{name:'Nome',exact:true}).fill('Dahua de teste');
 await info().getByRole('combobox',{name:'Site',exact:true}).selectOption('site');
 await info().getByRole('combobox',{name:'Tipo de conexão'}).selectOption('dahua');
 await info().getByRole('combobox',{name:'Protocolo'}).selectOption('https');
 assert(await info().getByRole('spinbutton',{name:'Porta da câmera'}).inputValue()==='443','Protocol change lost the default port');
 await info().getByRole('textbox',{name:'Endereço',exact:true}).fill('192.0.2.40');
 await info().getByRole('textbox',{name:'Usuário',exact:true}).fill('operador');
 await info().getByLabel('Senha',{exact:true}).fill('fixture-only-password');
 await info().getByRole('button',{name:'Consultar câmera e perfis'}).click();
 await info().getByText(/API autenticada/).waitFor();
 await section('Streams');
 await streams().getByRole('button',{name:'Testar vídeo · Principal',exact:true}).click();
 await streams().getByText(/Vídeo acessível/).waitFor();
 await section('Informações');await section('Streams');
 await streams().getByText(/Vídeo acessível/).waitFor();
 await section('Informações');
 await page.screenshot({path:'output/playwright/camera-form/dahua-desktop-dark.png',fullPage:true});
 await save();await dialog().waitFor({state:'detached'});
 const dahua=await lastMutation();
 assert(dahua.body.connectionType==='dahua'&&dahua.body.connection.protocol==='https'&&dahua.body.connection.httpPort===443&&!('profiles' in dahua.body),'Dahua create payload changed');
 checks.push('Dahua creation, protocol defaults, API discovery from Informações, per-profile video tests and preserved results');

 await fresh();
 const hikvision=info().getByRole('combobox',{name:'Tipo de conexão'}).locator('option[value=hikvision]');
 if(await hikvision.evaluate(el=>!el.disabled)){
  await info().getByRole('textbox',{name:'Nome',exact:true}).fill('Hikvision de teste');
  await info().getByRole('combobox',{name:'Site',exact:true}).selectOption('site');
  await info().getByRole('combobox',{name:'Tipo de conexão'}).selectOption('hikvision');
  await info().getByRole('textbox',{name:'Endereço',exact:true}).fill('192.0.2.45');
  await info().getByRole('textbox',{name:'Usuário',exact:true}).fill('operador');
  await info().getByLabel('Senha',{exact:true}).fill('fixture-only-password');
  await info().getByRole('button',{name:'Consultar câmera e perfis'}).click();
  await info().getByText(/API autenticada/).waitFor();
  await section('Streams');
  await streams().getByRole('button',{name:'Testar vídeo · Principal',exact:true}).click();
  await streams().getByText(/Vídeo acessível/).waitFor();
  await save();await dialog().waitFor({state:'detached'});
  const createdHikvision=await lastMutation();
  assert(createdHikvision.body.connectionType==='hikvision'&&createdHikvision.body.connection.host==='192.0.2.45','Hikvision connection payload changed');
  checks.push('Enabled Hikvision integration keeps connection fields, discovery, profile tests and create payload');
 } else await close();

 await edit('Dahua existente');
 await page.evaluate(()=>window.cameraFormQA.failDiscovery(true));
 await info().getByRole('button',{name:'Consultar câmera e perfis'}).click();
 await info().getByRole('alert').filter({hasText:'A API recusou as credenciais'}).waitFor();
 await section('Streams');await streams().getByRole('alert').filter({hasText:'A API recusou as credenciais'}).waitFor();
 await close();
 await page.evaluate(()=>{window.cameraFormQA.failDiscovery(false);window.cameraFormQA.delayDiscovery(300);});
 await edit('Dahua existente');
 await info().getByRole('button',{name:'Consultar câmera e perfis'}).click();
 await info().getByRole('textbox',{name:'Endereço',exact:true}).fill('192.0.2.99');
 await page.waitForTimeout(500);
 assert(await info().getByText(/API autenticada/).count()===0,'Stale discovery result survived a connection change');
 await close();
 checks.push('Discovery errors in both sections and stale-response rejection after connection changes');

 await edit('Dahua existente');
 await dialog().getByRole('tab',{name:'Informações',exact:true}).focus();
 await page.keyboard.press('ArrowDown');await streams().waitFor();
 await page.keyboard.press('End');await panel('Diagnóstico').waitFor();
 assert(await dialog().getByRole('button',{name:'Salvar informações'}).count()===0,'Diagnostic section exposed save');
 await panel('Diagnóstico').getByRole('button',{name:'Testar conexão',exact:true}).click();
 await panel('Diagnóstico').getByText('Fonte de vídeo acessível',{exact:true}).waitFor();
 await section('Informações');
 await info().getByRole('combobox',{name:'Tipo de conexão'}).selectOption('rtsp');
 await info().getByRole('textbox',{name:'Endereço RTSP principal'}).fill('rtsp://192.0.2.40/replacement');
 await save();await dialog().waitFor({state:'detached'});
 updated=await lastMutation();assert(updated.body.source.connectionType==='rtsp'&&updated.body.source.profiles[0].url.endsWith('/replacement'),'Connection type change lost the replacement payload');
 checks.push('Keyboard navigation, existing diagnostics and connection type replacement');

 await edit('Site restrito');
 assert(await info().getByRole('combobox',{name:'Site',exact:true}).inputValue()==='restricted-site','Restricted saved site was lost');
 await close();
 await fresh();
 await info().getByRole('textbox',{name:'Nome',exact:true}).fill('Conflito de teste');
 await info().getByRole('combobox',{name:'Site',exact:true}).selectOption('site');
 await info().getByRole('textbox',{name:'Endereço RTSP principal'}).fill('rtsp://192.0.2.40/main');
 await page.evaluate(()=>window.cameraFormQA.failSave(true));
 await save();await dialog().getByRole('alert').waitFor();
 assert(await info().getByRole('textbox',{name:'Nome',exact:true}).inputValue()==='Conflito de teste','Save error discarded entered values');
 await close();
 assert(await page.getByRole('button',{name:'+ Nova câmera',exact:true}).evaluate(el=>el===document.activeElement),'Dialog did not restore trigger focus');
 checks.push('Restricted current site, save errors, entered values and focus restoration');

 await page.goto('http://127.0.0.1:13119/?light');
 await page.getByRole('button',{name:'+ Nova câmera',exact:true}).click();
 assert(await layoutFits(),'Desktop dialog overflows');
 await page.screenshot({path:'output/playwright/camera-form/rtsp-desktop-light.png',fullPage:true});
 await close();
 for(const width of [390,320]){
  await page.setViewportSize({width,height:844});
  await edit('Dahua existente');
  assert(await layoutFits(),width+'px dialog overflows');
  const canScroll=await dialog().locator('.dialog-form-body').evaluate(el=>el.scrollHeight>el.clientHeight);
  assert(canScroll,'Long mobile form cannot scroll');
  await info().getByRole('textbox',{name:'Endereço',exact:true}).scrollIntoViewIfNeeded();
  await section('Streams');assert(await layoutFits(),'Mobile Streams overflows');
  await section('Informações');
  await page.screenshot({path:'output/playwright/camera-form/mobile-light-'+width+'.png',fullPage:true});
  await close();
 }
 await page.setViewportSize({width:1280,height:900});
 checks.push('Dark/light themes, desktop layout, 390px and 320px mobile scrolling and reachable footer');
 return {passed:true,checks};
 } catch(error) {
  await page.screenshot({path:'output/playwright/camera-form/failure.png',fullPage:true});
  throw new Error(JSON.stringify({passed:false,checks,error:String(error)}));
 }
}
