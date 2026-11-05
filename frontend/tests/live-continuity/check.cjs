async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 const click=name=>page.getByRole('button',{name,exact:true}).click();
 const waitVideos=async count=>page.waitForFunction(count=>[...document.querySelectorAll('.video-area video')].filter(v=>v.readyState>=2&&v.getVideoPlaybackQuality().totalVideoFrames>=2).length>=count,count);
 const settled=async(count,profile)=>page.waitForFunction(({count,profile})=>qa.sessions.size===count&&[...qa.sessions.values()].every(s=>!profile||s.profile===profile)&&document.querySelectorAll('.live-stream-video[data-presented="true"]').length===count,{count,profile});
 const save=()=>page.evaluate(()=>{window.savedVideos=[...document.querySelectorAll('.live-stream-video[data-presented="true"]')];window.savedStreams=window.savedVideos.map(v=>v.srcObject);qa.events.length=0;qa.maxSessions=qa.sessions.size;});
 const preserved=()=>page.evaluate(()=>window.savedVideos.every((v,i)=>v.isConnected&&v.srcObject===window.savedStreams[i]));
 const counts=()=>page.evaluate(()=>({opened:qa.events.filter(e=>e.type==='open').length,deleted:qa.events.filter(e=>e.type==='delete').length}));
 const report={};
 try {
 const native=page.url().endsWith('#native');
 await page.goto('http://127.0.0.1:13110'+(native?'#native':''));
 // Navigating only to another fragment may keep the previous fixture and its
 // deliberately revoked permissions alive. Always start with a fresh document.
 await page.reload();
 await click('Quatro câmeras');await waitVideos(4);
 await page.evaluate(()=>{window.originalVideos=[...document.querySelectorAll('.video-area video')];window.originalStreams=window.originalVideos.map(v=>v.srcObject);window.originalSessions=[...qa.sessions.keys()];qa.events.length=0;qa.held.add('main');});
 await click('Alternar maximização');
 await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'&&e.profile==='main'));
 let during=await page.evaluate(()=>({deleted:qa.events.filter(e=>e.type==='delete').length,opened:qa.events.filter(e=>e.type==='open').length,preserved:window.originalVideos.every((v,i)=>v.isConnected&&v.srcObject===window.originalStreams[i])}));
 assert(during.deleted===0&&during.opened===1&&during.preserved,'Maximizar encerrou ou recriou transmissões originais: '+JSON.stringify(during));
 await page.evaluate(()=>qa.release());await waitVideos(5);
 await page.screenshot({path:'output/playwright/live-continuity/maximized.png'});
 await click('Alternar maximização');
 await page.waitForFunction(()=>qa.sessions.size===4);
 assert(await page.evaluate(()=>window.originalVideos.every((v,i)=>v.isConnected&&v.srcObject===window.originalStreams[i])),'Restaurar recriou o mosaico');
 report.maximize=during;report.restore='original video elements and MediaStreams preserved';

 await save();await click('Três câmeras');await settled(3,'sub2');
 let events=await counts();assert(events.opened===0&&events.deleted===1,'Remoção na mesma faixa reconectou outras câmeras');
 assert(await page.evaluate(()=>window.savedVideos.slice(0,3).every((v,i)=>v.isConnected&&v.srcObject===window.savedStreams[i])),'Remoção afetou as câmeras restantes');
 report.removeSameBand=events;

 await save();await page.evaluate(()=>qa.held.add('main'));await click('Duas câmeras');
 await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'&&e.profile==='main'));
 events=await counts();assert(events.opened===1&&events.deleted===1,'Trocas de perfil não foram serializadas');
 assert(await page.evaluate(()=>window.savedVideos.slice(0,2).every((v,i)=>v.isConnected&&v.srcObject===window.savedStreams[i])),'Remoção entre faixas interrompeu vídeo antes do novo perfil');
 await page.evaluate(()=>qa.release());await settled(2,'main');await waitVideos(2);
 assert(await page.evaluate(()=>qa.maxSessions<=3),'Preparação serial excedeu uma sessão adicional');
 report.removeAcrossBands='old videos preserved until new profiles decoded';

 await save();await page.evaluate(()=>qa.held.add('sub2'));await click('Três câmeras');
 await page.waitForFunction(()=>qa.events.filter(e=>e.type==='open').length>=2);
 assert(await preserved(),'Adicionar câmera interrompeu as transmissões existentes');
 assert((await counts()).deleted===0,'Adicionar câmera encerrou sessões antes do novo perfil');
 await page.evaluate(()=>qa.release());await settled(3,'sub2');await waitVideos(3);
 assert(await page.evaluate(()=>qa.maxSessions<=4),'Adição excedeu uma sessão adicional além do mosaico final');
 report.addAcrossBands='old videos preserved until new profiles decoded';

 await save();await page.evaluate(()=>qa.failures.add('main'));await click('Principal na primeira');
 await page.getByText('Qualidade solicitada indisponível. Mantendo a transmissão atual.',{exact:true}).waitFor();
 assert(await preserved(),'Falha do novo perfil encerrou a transmissão anterior');
 assert((await counts()).deleted===0,'Falha de qualidade encerrou sessões saudáveis');
 await page.screenshot({path:'output/playwright/live-continuity/quality-failure.png'});
 await page.evaluate(()=>qa.failures.clear());await page.getByRole('button',{name:'Tentar novamente',exact:true}).click();
 await page.waitForFunction(()=>[...qa.sessions.values()].some(s=>s.cameraId==='cam1'&&s.profile==='main')&&qa.sessions.size===3);
 await waitVideos(3);report.profileFailure='kept playing; manual retry recovered';

 await save();await click('Alternar maximização');
 await page.waitForFunction(()=>document.querySelector('.live-grid').dataset.maximized==='true');
 assert((await counts()).opened===0&&await preserved(),'Maximizar perfil já selecionado criou conexão redundante');
 await click('Alternar maximização');report.explicitProfile='reused without additional session';

 await click('Automática na primeira');await settled(3,'sub2');await waitVideos(3);await save();
 await page.evaluate(()=>qa.failures.add('main'));await click('Alternar maximização');
 await page.getByText('Qualidade solicitada indisponível. Mantendo a transmissão atual.',{exact:true}).waitFor();
 assert(await preserved(),'Falha ao maximizar interrompeu o mosaico');
 await click('Alternar maximização');await page.evaluate(()=>qa.failures.clear());report.maximizeFailure='kept original mosaic';

 await save();await page.evaluate(()=>qa.delayed.add('main'));await click('Alternar maximização');
 await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'&&e.profile==='main'));
 await click('Alternar maximização');await page.evaluate(()=>qa.flushPosts());await settled(3,'sub2');
 assert(await preserved(),'Restaurar durante abertura de sessão afetou o mosaico');
 report.cancelLateSession='late POST result deleted without attaching video';

 await click('Duas câmeras');await settled(2,'main');await waitVideos(2);await save();
 await page.evaluate(()=>qa.held.add('sub2'));await click('Três câmeras');await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'));
 await click('Duas câmeras');await page.evaluate(()=>qa.release());await settled(2,'main');
 assert(await preserved(),'Mudanças rápidas de faixa descartaram as transmissões originais');
 await click('Três câmeras');await settled(3,'sub2');await waitVideos(3);
 report.rapidBandChanges='pending and queued profiles cancelled; originals reused';

 await save();await click('Substituir primeira');await settled(3,'sub2');await waitVideos(3);
 events=await counts();assert(events.opened===1&&events.deleted===1,'Substituição afetou sessões de outras câmeras');
 assert(await page.evaluate(()=>window.savedVideos.slice(1).every((v,i)=>v.isConnected&&v.srcObject===window.savedStreams[i+1])),'Substituição reconectou outras posições');
 report.replaceCamera=events;

 await save();await page.evaluate(()=>{qa.cameras.find(c=>c.id==='cam5').sourceVersion='2';window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'InventoryChanged'}}));});
 await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'));await settled(3,'sub2');
 events=await counts();assert(events.opened===1&&events.deleted===1,'Troca de fonte não reiniciou somente a câmera correta');
 report.sourceChange=events;

 await save();await page.evaluate(()=>window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'AuthorizationChanged'}})));
 await page.waitForFunction(()=>!document.querySelector('.live-camera-browser [aria-busy="true"]'));
 assert(await preserved(),'Atualização de catálogo recriou vídeos');assert((await counts()).opened===0,'Atualização inalterada abriu sessões');
 report.catalogRefresh='preserved';

 await save();await click('Alternar maximização');await waitVideos(4);
 await page.waitForTimeout(13500);
 events=await counts();assert(events.opened===1&&events.deleted===0&&await preserved(),'Watchdog reiniciou transmissões ocultas que ainda recebem vídeo');
 await click('Alternar maximização');await settled(3,'sub2');report.backgroundWatchdog='no reconnect after 13.5 seconds hidden';

 await save();await page.evaluate(()=>qa.delayed.add('main'));await click('Alternar maximização');await page.waitForFunction(()=>qa.events.some(e=>e.type==='open'));
 await click('Trocar tenant');await page.evaluate(()=>qa.flushPosts());await page.waitForFunction(()=>qa.sessions.size===0);
 report.tenantChange='all established, pending and late sessions closed';

 await click('Quatro câmeras');await settled(4,'sub2');await waitVideos(4);
 await page.evaluate(()=>{qa.deny=true;});await page.waitForFunction(()=>qa.sessions.size===0&&document.querySelectorAll('.video-area video').length===0);
 report.accessRevocation='all videos removed and sessions closed';
 return {transport:native?'native WebRTC loopback':'simulated signaling',...report};
 }catch(error){throw new Error(JSON.stringify({completed:report,error:error.message,state:await page.evaluate(()=>({sessions:[...qa.sessions.values()],denied:qa.deny,videos:document.querySelectorAll('.video-area video').length}))}));}
}
