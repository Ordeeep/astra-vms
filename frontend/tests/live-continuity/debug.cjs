async page => {
 const {expect}=await import("@playwright/test");
 const native=page.url().endsWith("#native");
 const id="1234abcd".repeat(6);
 await page.goto("http://127.0.0.1:13110/?debugCameraId="+id+(native?"#native":""));
 const debug=page.getByRole("button",{name:"Debug do mosaico"});
 const card=page.locator('[data-live-position="0"]');
 const metrics=page.getByRole("group",{name:"Métricas da transmissão ao vivo"});
 const title=card.locator(".live-camera-debug-name");
 await expect(debug).toHaveAttribute("aria-pressed","false");
 await page.getByRole("button",{name:"Quatro câmeras",exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('video[data-presented="true"]').length===4);
 await expect(metrics).toHaveCount(0);
 await page.evaluate(()=>{qa.events.length=0;});
 await debug.press("Space");
 await expect(debug).toHaveAttribute("aria-pressed","true");
 await expect(metrics).toHaveCount(4);
 await expect(title).toHaveText("[1234abcd] Câmera 1");
 await title.focus();
 const idTooltip=page.getByRole("tooltip").filter({hasText:id});
 await expect(idTooltip).toBeVisible();
 await title.press("Escape");
 await expect(idTooltip).toHaveCount(0);
 await expect(card.getByRole("group",{name:"Métricas da transmissão ao vivo"})).toHaveText(/\d+ FPS.*320 × 180.*[A-Z0-9]/);
 for (const icon of ["gauge","scan-line","cpu","arrow-down-to-line"]) {
  await expect(card.locator(`.live-debug-metrics .lucide-${icon}`)).toBeVisible();
 }
 const bitrate=card.getByText("Bitrate recebido",{exact:true}).locator("..");
 await expect(bitrate).toContainText(native?/\d+,\d{2} Mbps/:"1,20 Mbps");
 expect(await page.evaluate(()=>qa.events)).toEqual([]);

 for (const width of [390,768,1280,1600]) {
  await page.setViewportSize({width,height:900});
  const band=card.locator(".live-debug-metrics");
  await expect(band).toBeVisible();
  expect(await band.evaluate(element=>{
   const video=element.closest(".video-area").getBoundingClientRect();
   const overlay=element.getBoundingClientRect();
   return overlay.top===video.top&&overlay.width===video.width&&element.scrollWidth===element.clientWidth;
  })).toBe(true);
 }

 if (!native) {
  await expect(card.locator(".live-debug-metrics")).toContainText("VP8");
  await page.evaluate(()=>qa.held.add("main"));
  await page.getByRole("button",{name:"Alternar maximização",exact:true}).click();
  await page.waitForFunction(()=>[...qa.sessions.values()].some(session=>session.profile==="main"));
  await expect(metrics).toHaveCount(1);
  await expect(metrics).toContainText("VP8");
  await expect(bitrate).toContainText("1,20 Mbps");
  await page.evaluate(()=>qa.release());
  await expect(metrics).toHaveText(/10 FPS.*320 × 180.*H\.264/);
  await expect(bitrate).toContainText("2,48 Mbps");
  await page.getByRole("button",{name:"Alternar maximização",exact:true}).click();
  await expect(metrics).toHaveCount(4);
 }

 await page.getByRole("button",{name:"Vídeo Wall",exact:true}).click();
 await expect(page.locator(".live-workspace")).toHaveAttribute("data-wall-controls-visible","false");
 await expect(title).toBeVisible();
 await expect(card.locator(".live-debug-metrics")).toBeVisible();
 await page.keyboard.press("Escape");
 await debug.press("Enter");
 await expect(debug).toHaveAttribute("aria-pressed","false");
 await expect(metrics).toHaveCount(0);
 await expect(title).toHaveCount(0);
 return {native, debug:"passed"};
}
