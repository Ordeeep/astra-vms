async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 if(page.url().endsWith('#native'))await page.reload();
 else await page.goto('http://127.0.0.1:13110/');
 await page.getByRole('button',{name:'Quatro câmeras',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('video[data-presented="true"]').length===4);

 const tile=position=>page.getByRole('group',{name:`Posição ${position} do mosaico`,exact:true});
 const first=tile(1),video=first.locator('video[data-presented="true"]');
 const measure=()=>video.evaluate(element=>{
  const {width,height,x,y}=element.getBoundingClientRect();
  const area=element.closest('.video-area').getBoundingClientRect();
  return {fit:getComputedStyle(element).objectFit,width,height,x,y,
   cellWidth:area.width,cellHeight:area.height,cellX:area.x,cellY:area.y};
 });
 const setMode=async mode=>{
  await page.evaluate(displayMode=>{
   qa.cameras[0].displayMode=displayMode;
   window.dispatchEvent(new CustomEvent('vms:change',{detail:{type:'CameraStatusChanged'}}));
  },mode);
  await first.locator(`[data-display-mode="${mode}"]`).waitFor();
 };
 const close=(a,b)=>Math.abs(a-b)<1;
 assert((await measure()).fit==='contain','Câmera sem modo salvo perdeu a proporção original');
 await video.evaluate(element=>{window.displayVideo=element;window.displayStream=element.srcObject;qa.events.length=0;});

 await setMode('cover');
 const cover=await measure();
 assert(cover.fit==='cover'&&close(cover.width,cover.cellWidth)&&close(cover.height,cover.cellHeight),'Espalhar pela célula deixou espaço no card');
 assert(await tile(2).locator('[data-display-mode="contain"]').count()===1,'O modo alterou outra câmera');

 await setMode('4:3');
 for(const width of [1600,1280,768,390]){
  await page.setViewportSize({width,height:900});
  const fixed=await measure();
  assert(fixed.fit==='fill'&&close(fixed.width,fixed.height*4/3),`A imagem deixou de ter proporção 4:3 em ${width}px`);
  assert(fixed.width<=fixed.cellWidth+1&&fixed.height<=fixed.cellHeight+1,`A imagem 4:3 transbordou em ${width}px`);
  assert(close(fixed.x-fixed.cellX,(fixed.cellWidth-fixed.width)/2)&&close(fixed.y-fixed.cellY,(fixed.cellHeight-fixed.height)/2),`A imagem 4:3 saiu do centro em ${width}px`);
 }
 await page.setViewportSize({width:1280,height:900});
 const fixed=await measure();
 const area=first.locator('.video-area');
 await first.getByRole('button',{name:/Zoom digital/}).click();
 await area.press('+');
 const zoomed=await measure();
 assert(close(zoomed.width,fixed.width*1.25)&&close(zoomed.width,zoomed.height*4/3),'O zoom não manteve a imagem em 4:3');
 await area.press('ArrowLeft');
 assert(close((await measure()).x-zoomed.x,fixed.cellWidth*.05),'O deslocamento usou a largura da imagem em vez da célula');
 await area.press('Escape');
 await setMode('contain');
 assert((await measure()).fit==='contain','Manter a proporção não restaurou o enquadramento');
 assert(await video.evaluate(element=>element===window.displayVideo&&element.srcObject===window.displayStream&&qa.events.length===0),'Alterar o enquadramento reiniciou a transmissão');
 return {passed:['default aspect ratio','fill the cell','fixed 4:3 at four viewport widths','digital zoom and pan in 4:3','per-camera isolation','live stream continuity']};
}
