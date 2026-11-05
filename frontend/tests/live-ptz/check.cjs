async page => {
 const assert=(value,message)=>{if(!value)throw new Error(message);};
 const checks=[];
 const load=async()=>{await page.goto("http://127.0.0.1:13125");await page.getByRole("group",{name:"Controle PTZ de Entrada",exact:true}).waitFor();await page.getByRole("button",{name:"Mover para cima",exact:true}).click({trial:true});};
 const count=async method=>page.evaluate(method=>ptzQA.requests.filter(r=>r.method===method).length,method);
 const waitCount=async(method,n)=>page.waitForFunction(({method,n})=>ptzQA.requests.filter(r=>r.method===method).length>=n,{method,n});
 await load();
 const up=page.getByRole("button",{name:"Mover para cima",exact:true});
 await up.focus();await page.keyboard.down("Space");
 await waitCount("POST",1);await waitCount("PUT",1);
 await page.keyboard.down("Space");
 assert(await count("POST")===1,"keyboard autorepeat started more commands");
 await page.keyboard.up("Space");
 await waitCount("DELETE",1);
 const first=await page.evaluate(()=>ptzQA.requests.find(r=>r.method==="POST"));
 assert(first.path.includes("/a/ptz/")&&first.body.pan===0&&first.body.tilt===.6,"wrong camera or direction/speed");
 checks.push("keyboard hold/release, default speed, renewal and autorepeat");

 const box=await up.boundingBox();await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();
 await waitCount("POST",2);await page.mouse.move(500,500);await page.mouse.up();await waitCount("DELETE",2);
 checks.push("pointer capture stops after release outside the wheel");
 await page.getByRole("slider",{name:"Velocidade PTZ"}).focus();await page.keyboard.press("End");
 const diagonal=page.getByRole("button",{name:"Mover para baixo e esquerda",exact:true});await diagonal.focus();await page.keyboard.down("Enter");await waitCount("POST",3);await page.keyboard.up("Enter");await waitCount("DELETE",3);
 const last=await page.evaluate(()=>ptzQA.requests.filter(r=>r.method==="POST").at(-1).body);
 assert(last.pan===-1&&last.tilt===-1,"diagonal or max speed mapped incorrectly");
 await page.getByRole("button",{name:"Posição inicial",exact:true}).click();await waitCount("POST",4);
 assert(await page.evaluate(()=>ptzQA.requests.filter(r=>r.method==="POST").at(-1).body.home===true),"Home was not generic Home action");
 await page.evaluate(()=>window.dispatchEvent(new Event("blur")));await waitCount("DELETE",4);
 checks.push("diagonal, maximum speed, Home and blur cleanup");

 await page.getByRole("button",{name:"Ativar Pátio",exact:true}).click();await page.getByRole("group",{name:"Controle PTZ de Pátio",exact:true}).waitFor();
 await page.getByRole("button",{name:"Aumentar zoom",exact:true}).click({trial:true});
 assert(await up.isDisabled(),"zoom-only camera enabled pan/tilt");
 await page.getByRole("button",{name:"Maximizar Pátio",exact:true}).click();await page.getByRole("button",{name:"Ativar Entrada",exact:true}).click();
 assert(await page.getByRole("group",{name:"Controle PTZ de Pátio",exact:true}).count()===1,"active selection overrode maximized camera");
 checks.push("partial resources and maximized camera takes priority");

 await load();await page.evaluate(()=>ptzQA.delay(500));
 await up.focus();await page.keyboard.down("Space");await waitCount("POST",1);await page.keyboard.up("Space");
 await page.getByRole("button",{name:"Ativar Pátio",exact:true}).click();await waitCount("DELETE",1);
 assert(await page.evaluate(()=>ptzQA.requests.find(r=>r.method==="DELETE").path.includes("/a/ptz/commands/command-1")),"late start stopped replacement camera");
 assert(await count("PUT")===0,"released delayed command was renewed");
 checks.push("late start is stopped on original camera after target change");

 await load();await up.focus();await page.keyboard.down("Space");await waitCount("POST",1);
 await page.evaluate(()=>ptzQA.supersede());await page.getByRole("status").filter({hasText:"Outro comando assumiu"}).waitFor();
 const renewals=await count("PUT");await page.waitForTimeout(850);
 assert(await count("POST")===1&&await count("PUT")===renewals,"old gesture reclaimed control after supersession");
 await page.keyboard.up("Space");
 checks.push("superseded command stops renewing and never restarts automatically");

 await load();await up.focus();await page.keyboard.down("Space");await waitCount("POST",1);await page.evaluate(()=>ptzQA.revoke());
 await page.getByRole("status").filter({hasText:"Sem permissão"}).waitFor();await waitCount("DELETE",1);await page.keyboard.up("Space");
 assert(await up.isDisabled(),"revoked PTZ stayed enabled");
 checks.push("permission revocation stops and disables control");

 await load();await up.focus();await page.keyboard.down("Space");await waitCount("POST",1);
 await page.getByRole("button",{name:"Trocar tenant",exact:true}).click();await waitCount("DELETE",1);await page.keyboard.up("Space");
 const cleanup=await page.evaluate(()=>ptzQA.requests.find(r=>r.method==="DELETE"));
 assert(cleanup.path.includes("/tenants/tenant/")&&cleanup.context==="context-tenant","cleanup borrowed new tenant context");
 await page.getByRole("button",{name:"Limpar mosaico",exact:true}).click();await page.getByRole("status").filter({hasText:"Selecione um quadro"}).waitFor();
 assert(await up.isDisabled(),"empty mosaic enabled movement");
 checks.push("tenant cleanup freezes original context; empty selection disables controls");
 await load();await page.setViewportSize({width:390,height:844});await page.screenshot({path:"output/playwright/live-ptz/mobile.png",fullPage:true});
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),"mobile horizontal overflow");
 await page.setViewportSize({width:1280,height:800});await page.screenshot({path:"output/playwright/live-ptz/desktop.png",fullPage:true});
 checks.push("mobile and desktop render");
 return {passed:true,checks};
}
