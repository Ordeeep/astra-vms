async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 await page.goto('http://127.0.0.1:13110');
 await page.getByRole('button',{name:'Quatro câmeras',exact:true}).click();
 await page.waitForFunction(()=>qa.sessions.size===4&&document.querySelectorAll('video[data-presented="true"]').length===4);
 await page.getByRole('button',{name:'Substream na primeira',exact:true}).click();
 await page.evaluate(()=>{
  const originalFetch=window.fetch;
  const originalSessions=[...qa.sessions.values()];
  const otherVideos=[...document.querySelectorAll('[data-live-position]:not([data-live-position="0"]) video[data-presented="true"]')].map(video=>({video,stream:video.srcObject}));
  const probe={originalSessions,otherVideos,renewalError:503,openError:503,failures:[],attempts:[]};
  qa.reconnect=probe;
  const error=(status,code)=>new Response(JSON.stringify({code}),{status,headers:{'Content-Type':'application/json','X-VMS-Contract':'1'}});
  window.fetch=async(input,options)=>{
   const path=String(input),method=options?.method;
   if(method==='PUT'&&path.includes('/live-sessions/')&&probe.renewalError&&qa.sessions.get(path.split('/').at(-1))?.cameraId==='cam1'){
    const status=probe.renewalError;
    probe.renewalError=0;
    probe.failures.push(performance.now());
    return error(status,status===410?'live_session_ended':'media_unavailable');
   }
   if(method==='POST'&&path.endsWith('/live-sessions')&&JSON.parse(options.body).cameraId==='cam1'){
    const status=probe.openError;
    probe.attempts.push({at:performance.now(),status});
    if(status===503)probe.openError=0;
    if(status)return error(status,status===403?'forbidden':'media_unavailable');
   }
   return originalFetch(input,options);
  };
 });
 await page.getByRole('button',{name:'Falha na conexão de Câmera 1. Reconectar'}).waitFor();
 await page.waitForFunction(()=>qa.reconnect.attempts.length>=2&&qa.sessions.size===4&&document.querySelector('[data-live-position="0"] video[data-presented="true"]'),null,{timeout:15000});
 const recovery=await page.evaluate(()=>{
  const {originalSessions,otherVideos,failures,attempts}=qa.reconnect;
  return {
   delays:[attempts[0].at-failures[0],attempts[1].at-attempts[0].at],
   statuses:attempts.map(attempt=>attempt.status),
   failedSessionClosed:!qa.sessions.has(originalSessions.find(session=>session.cameraId==='cam1').id),
   otherSessionsPreserved:originalSessions.filter(session=>session.cameraId!=='cam1').every(session=>qa.sessions.has(session.id)),
   otherVideosPreserved:otherVideos.length===3&&otherVideos.every(({video,stream})=>video.isConnected&&video.srcObject===stream),
  };
 });
 assert(recovery.delays.every(delay=>delay>=4900&&delay<6000),`Expected about five seconds between attempts: ${recovery.delays}`);
 assert(JSON.stringify(recovery.statuses)==='[503,0]',`Unexpected reconnect attempts: ${recovery.statuses}`);
 assert(recovery.failedSessionClosed&&recovery.otherSessionsPreserved&&recovery.otherVideosPreserved,'Recovery changed healthy streams or left the failed session open');

 await page.evaluate(()=>Object.assign(qa.reconnect,{renewalError:410,openError:403}));
 await page.getByLabel('Acesso à transmissão encerrado',{exact:true}).waitFor({timeout:8000});
 await page.waitForTimeout(5200);
 const denied=await page.evaluate(()=>({attempts:qa.reconnect.attempts.map(attempt=>attempt.status),active:qa.sessions.size}));
 assert(JSON.stringify(denied.attempts)==='[503,0,403]'&&denied.active===3,'Retried after access was denied');
 return {...recovery,denied};
}
