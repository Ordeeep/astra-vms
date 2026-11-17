async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 await page.goto('http://127.0.0.1:13110'+(page.url().endsWith('#native')?'#native':''));
 await page.reload();
 await page.getByRole('button',{name:'Quatro câmeras',exact:true}).click();
 await page.waitForFunction(()=>qa.sessions.size===4&&document.querySelectorAll('video[data-presented="true"]').length===4);
 await page.evaluate(()=>{
  window.previousSession=[...qa.sessions.values()].find(s=>s.cameraId==='cam1').id;
  window.previousVideo=document.querySelector('.live-cell video[data-presented="true"]');
  window.previousStream=window.previousVideo.srcObject;
  qa.held.add('main');
 });
 await page.getByRole('button',{name:'Alternar maximização',exact:true}).click();
 await page.waitForFunction(()=>[...qa.sessions.values()].some(s=>s.cameraId==='cam1'&&s.profile==='main'));
 assert(await page.evaluate(()=>qa.sessions.has(window.previousSession)),'Previous stream stopped before the replacement was ready');
 await page.evaluate(()=>qa.release());
 await page.waitForFunction(()=>[...document.querySelector('.live-cell').querySelectorAll('video[data-presented="true"]')].some(video=>video.videoWidth>0&&video.srcObject!==window.previousStream));
 // Allow effect cleanup to commit, then measure the exact resource leak.
 await page.waitForTimeout(800);
 const state=await page.evaluate(()=>({
  sessions:[...qa.sessions.values()].filter(s=>s.cameraId==='cam1'),
  previousClosed:!qa.sessions.has(window.previousSession),
  previousPeerClosed:qa.events.some(e=>e.type==='close'&&e.id===window.previousSession),
  videos:document.querySelector('.live-cell').querySelectorAll('video').length,
  previousVideoDetached:window.previousVideo.srcObject===null,
 }));
 assert(state.previousClosed&&state.previousPeerClosed&&state.sessions.length===1&&state.videos===1,'Hidden stream still running after quality switch: '+JSON.stringify(state));
 assert(state.previousVideoDetached,'Unmounted video still retains the previous media stream');
 await page.getByRole('button',{name:'Alternar maximização',exact:true}).click();
 await page.waitForFunction(()=>qa.sessions.size===4&&[...qa.sessions.values()].every(s=>s.profile==='sub2')&&document.querySelectorAll('video[data-presented="true"]').length===4);
 return {maximized:state,restored:'one session per camera'};
}
