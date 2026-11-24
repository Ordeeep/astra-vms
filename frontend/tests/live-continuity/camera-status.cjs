async page => {
 const {expect} = await import('@playwright/test');
 await page.goto('http://127.0.0.1:13110/');
 await page.getByRole('button',{name:'Quatro câmeras',exact:true}).click();
 await page.getByRole('button',{name:'Expandir todos os sites',exact:true}).click();

 const row = number => page.getByRole('button',{name:`Arrastar Câmera ${number}`,exact:true});
 const icon = number => row(number).locator('.live-camera-status-icon > svg:first-child');
 const mark = number => row(number).locator('.live-camera-status-icon > svg:nth-child(2)');
 const dot = number => icon(number).locator('path[d="M7 9h.01"]');
 const expectStatus = (number,status,timeout) => expect(row(number)).toHaveAttribute('data-camera-status',status,{timeout});
 await expectStatus(1,'unknown');
 await page.evaluate(() => {
  const validUntil = new Date(Date.now()+60_000).toISOString();
  Object.assign(qa.cameras[0],{availability:{state:'online',validUntil},recordingActive:true});
  Object.assign(qa.cameras[1],{availability:{state:'online',validUntil},recordingActive:false});
  Object.assign(qa.cameras[2],{availability:{state:'offline',validUntil},recordingActive:true});
  Object.assign(qa.cameras[3],{availability:{state:'online',validUntil:'2026-01-01T00:00:00Z'},recordingActive:true});
  Object.assign(qa.cameras[4],{effectiveActive:false});
  window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'CaptureStateChanged'}}));
 });
 await expectStatus(1,'recording',2000);
 await expectStatus(2,'online');
 await expectStatus(3,'offline');
 await expectStatus(4,'unknown');
 await expectStatus(5,'suspended');
 await expect(icon(1)).toHaveClass(/lucide-cctv\b/);
 await expect(mark(1)).toHaveCount(0);
 await expect(dot(1)).toHaveCSS('animation-name','live-camera-recording-pulse');
 await expect(icon(1)).toHaveCSS('animation-name','none');
 await page.emulateMedia({reducedMotion:'reduce'});
 await expect(dot(1)).toHaveCSS('animation-name','none');
 await expect(dot(1)).toHaveCSS('opacity','1');
 await page.emulateMedia({reducedMotion:'no-preference'});
 await expect(mark(2)).toHaveCount(0);
 await expect(icon(2)).toHaveClass(/lucide-cctv\b/);
 await expect(icon(3)).toHaveClass(/lucide-cctv-off/);
 await expect(dot(3)).toHaveCSS('animation-name','none');
 await expect(mark(4)).toHaveClass(/lucide-circle/);
 for(const number of [1,2,3]){
  await expect(row(number)).toHaveText(`Câmera ${number}`);
 }
 await row(1).focus();
 await expect(page.getByRole('tooltip')).toHaveText('Online · Gravando');
 await expect(row(3)).toHaveAccessibleDescription(/Offline/);
 await page.keyboard.press('Escape');
 await expect(page.getByRole('tooltip')).toBeHidden();

 await page.addStyleTag({content:'.live-camera-item { transition: none !important; }'});
 const colors = async dark => {
  await page.evaluate(dark => document.documentElement.classList.toggle('dark',dark),dark);
  const borders = await Promise.all([1,2,3,4].map(number => row(number).evaluate(element => {
   const style = getComputedStyle(element);
   return {color:style.borderLeftColor,width:style.borderLeftWidth};
  })));
  expect(new Set(borders.map(border => border.color)).size).toBe(4);
  expect(borders.every(border => border.width === '2px')).toBe(true);
  await expect(dot(1)).toHaveCSS('stroke',borders[2].color);
  await expect(dot(2)).toHaveCSS('stroke',borders[0].color);
  await expect(icon(2)).toHaveCSS('color',borders[0].color);
  await expect(icon(3)).toHaveCSS('color',borders[2].color);
  await expect(icon(1)).toHaveCSS('color',borders[0].color);
  const onlineBackground = await row(2).evaluate(element => getComputedStyle(element).backgroundColor);
  await expect(row(1)).not.toHaveCSS('background-color',onlineBackground);
  return borders.map(border => border.color);
 };
 const lightColors = await colors(false);
 const darkColors = await colors(true);
 expect(darkColors).not.toEqual(lightColors);
 await row(1).press('Enter');
 await expect(row(1)).toHaveAttribute('aria-pressed','true');
 await expect(row(1)).toHaveCSS('border-left-color',darkColors[0]);
 await row(1).press('Escape');

 const video = page.locator('.live-stream-video[data-presented="true"]').first();
 await video.waitFor();
 const stream = await video.evaluateHandle(element => element.srcObject);
 const started = Date.now();
 await page.evaluate(() => { qa.cameras[0].recordingActive=false; });
 await expectStatus(1,'online',5500);
 await expect(dot(1)).toHaveCSS('animation-name','none');
 await expect(row(1)).toHaveCSS('border-left-color',darkColors[1]);
 const pollingMs = Date.now()-started;

 const setCatalogUnavailable = unavailable => page.evaluate(unavailable => {
  qa.catalogUnavailable=unavailable;
  window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'CaptureStateChanged'}}));
 },unavailable);
 await setCatalogUnavailable(true);
 await expectStatus(1,'unknown',2000);
 await setCatalogUnavailable(false);
 await expectStatus(1,'online',2000);
 expect(await video.evaluate((element,stream) => element.srcObject===stream,stream)).toBe(true);
 for(const width of [390,768,1280,1600]){
  await page.setViewportSize({width,height:900});
  await expect(icon(1)).toBeVisible();
  expect((await icon(1).boundingBox()).width).toBe(14);
 }
 return {lightColors,darkColors,pollingMs};
}
