async page=>{
 await page.goto("http://127.0.0.1:13125");
 const up=page.getByRole("button",{name:"Mover para cima",exact:true}),zoom=page.getByRole("button",{name:"Aumentar zoom",exact:true});
 await up.click({trial:true});await up.focus();await page.keyboard.down("Space");
 await page.waitForFunction(()=>ptzQA.requests.filter(r=>r.method==="POST").length===1);
 const box=await zoom.boundingBox();await page.mouse.move(box.x+box.width/2,box.y+box.height/2);await page.mouse.down();
 await page.waitForFunction(()=>ptzQA.requests.filter(r=>r.method==="POST").length===2);
 await page.waitForFunction(()=>ptzQA.requests.filter(r=>r.method==="DELETE").length===1);
 await page.keyboard.up("Space");await page.waitForTimeout(150);
 if(await page.evaluate(()=>ptzQA.requests.filter(r=>r.method==="DELETE").length)!==1)throw new Error("Releasing an old keyboard gesture interrupted newer pointer control");
 await page.mouse.up();await page.waitForFunction(()=>ptzQA.requests.filter(r=>r.method==="DELETE").length===2);
 await page.goto("http://127.0.0.1:13125");await up.click({trial:true});await page.evaluate(()=>ptzQA.delay(500));
 await up.focus();await page.keyboard.down("Space");await page.waitForFunction(()=>ptzQA.requests.some(r=>r.method==="POST"));
 await page.getByRole("button",{name:"Trocar tenant",exact:true}).click();await page.keyboard.up("Space");
 await page.waitForFunction(()=>ptzQA.requests.some(r=>r.method==="DELETE"&&r.path.includes("/tenants/tenant/")),{},{timeout:2500});
 const cleanup=await page.evaluate(()=>ptzQA.requests.find(r=>r.method==="DELETE"));
 if(cleanup.context!=="context-tenant")throw new Error("Late start cleanup used another context");
 return {passed:true,checks:["old keyboard release cannot stop newer pointer gesture","late POST after tenant switch is immediately stopped with original context"]};
}
