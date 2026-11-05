async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 const native=page.url().endsWith('#native');
 const fresh=async()=>{await page.goto('http://127.0.0.1:13110'+(native?'#native':''));await page.reload();};
 const click=name=>page.getByRole('button',{name,exact:true}).click();
 const settled=(count,profile)=>page.waitForFunction(({count,profile})=>qa.sessions.size===count&&[...qa.sessions.values()].every(s=>s.profile===profile)&&document.querySelectorAll('.live-stream-video[data-presented="true"]').length===count,{count,profile},{timeout:8000});
 const report={};
 await fresh();
 await page.evaluate(()=>qa.signalFailures.add('sub3'));
 await click('Cinco câmeras');
 await settled(5,'sub2');
 const recovered=await page.evaluate(()=>({opened:qa.events.filter(e=>e.type==='open').map(e=>e.profile),deleted:qa.events.filter(e=>e.type==='delete').length}));
 assert(recovered.opened.filter(p=>p==='sub3').length===5&&recovered.opened.filter(p=>p==='sub2').length===5&&recovered.deleted===5,'Fallback leaked or duplicated sessions: '+JSON.stringify(recovered));
 report.signaling503='five streams recovered on sub2; failed sessions closed';

 await page.evaluate(()=>{window.savedVideos=[...document.querySelectorAll('.live-stream-video[data-presented="true"]')];qa.events.length=0;qa.signalFailures.add('main');});
 await click('Principal na primeira');
 await page.getByText('Qualidade solicitada indisponível. Mantendo a transmissão atual.',{exact:true}).waitFor();
 const explicit=await page.evaluate(()=>({preserved:window.savedVideos.every(v=>v.isConnected&&v.srcObject),opened:qa.events.filter(e=>e.type==='open').map(e=>e.profile)}));
 assert(explicit.preserved&&explicit.opened.length===1&&explicit.opened[0]==='main','Explicit selection fell back or interrupted video: '+JSON.stringify(explicit));
 report.explicit='no profile substitution; original video preserved';

 await fresh();await page.evaluate(()=>{qa.unavailableProfiles.add('sub3');qa.signalFailures.add('sub2');});
 await click('Cinco câmeras');await settled(5,'main');
 assert(await page.evaluate(()=>qa.requests.length===10&&qa.requests.filter(r=>r.profile==='sub3').length===5&&qa.requests.filter(r=>r.profile==='main').length===5),'Backend-selected fallback was retried or skipped incorrectly');
 report.backendFallback='continued after actual sub2 to main without revisiting sub3';

 await fresh();await page.evaluate(()=>['sub3','sub2','main'].forEach(p=>qa.signalFailures.add(p)));
 await click('Cinco câmeras');
 await page.waitForFunction(()=>qa.events.filter(e=>e.type==='open').length===15&&qa.sessions.size===0&&document.querySelectorAll('button.video-failure').length===5,null,{timeout:8000});
 await page.waitForTimeout(500);
 assert(await page.evaluate(()=>qa.events.filter(e=>e.type==='open').length===15&&qa.sessions.size===0),'Exhausted fallback looped or retained sessions');
 report.exhaustion='three allowed profiles tried once per camera; no sessions leaked';

 await fresh();await page.evaluate(()=>{qa.deny=true;});await click('Cinco câmeras');
 await page.waitForTimeout(600);
 assert(await page.evaluate(()=>qa.requests.length===5&&qa.events.filter(e=>e.type==='open').length===0&&qa.sessions.size===0),'Forbidden request attempted alternate profiles');
 report.denied='no fallback after forbidden';
 return report;
}
