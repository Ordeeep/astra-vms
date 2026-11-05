async page=>{
 const cases=[];
 for(const viewport of [{width:1280,height:720},{width:390,height:844}]){
  await page.setViewportSize(viewport);await page.goto("http://127.0.0.1:13125/?panel");
  const trigger=page.getByRole("button",{name:"Controle PTZ",exact:true});await trigger.waitFor();
  for(let attempt=0;attempt<4;attempt++){
   await trigger.click();await page.waitForTimeout(300);
   const samples=await page.evaluate(async()=>{
    const content=document.querySelector('.live-ptz-content');
    const trigger=document.querySelector('.live-ptz-trigger');
    const samples=[];
    const measure=()=>samples.push({height:content.getBoundingClientRect().height,scroll:document.documentElement.scrollHeight,body:document.body.scrollHeight,visible:!content.hidden});
    measure();trigger.click();
    const start=performance.now();while(performance.now()-start<450){await new Promise(requestAnimationFrame);measure();}
    return samples;
   });
   cases.push({viewport,attempt,initial:samples[0],maxScroll:Math.max(...samples.map(s=>s.scroll)),heights:samples.map(s=>Math.round(s.height))});
   const scrollJump=samples.some(s=>s.scroll>samples[0].scroll+1);
   const heightJump=samples.some((s,i)=>i&&s.height>samples[i-1].height+1);
   if(scrollJump||heightJump)throw new Error(JSON.stringify({scrollJump,heightJump,cases}));
  }
 }
 return {passed:true,cases:cases.map(({heights,...summary})=>summary)};
}
