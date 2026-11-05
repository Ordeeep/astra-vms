async page => {
 const assert=(ok,message)=>{if(!ok)throw new Error(message);};
 const noOverlap=async()=>{
  const toggle=await page.getByRole('button',{name:'Recuar listagem',exact:true}).boundingBox();
  for(const close of await page.locator('.live-camera-close:visible').all()){
   const box=await close.boundingBox();
   assert(!(toggle.x<box.x+box.width&&toggle.x+toggle.width>box.x&&toggle.y<box.y+box.height&&toggle.y+toggle.height>box.y),'Panel toggle overlaps the camera close button');
  }
 };
 await page.goto('http://127.0.0.1:13110/');
 await page.getByRole('button',{name:'Quatro câmeras',exact:true}).click();
 await page.locator('.live-camera-close').nth(3).waitFor();
 await noOverlap();
 const marked=page.locator('.live-camera-item[data-in-mosaic="true"]');
 assert(await marked.count()===4,'All cameras in the mosaic must be highlighted');
 await page.getByRole('group',{name:'Posição 2 do mosaico',exact:true}).click();
 assert(await marked.count()===4,'Changing the active position must retain all highlights');
 await page.getByRole('button',{name:'Fechar câmera da posição 1',exact:true}).click();
 assert(await marked.count()===3,'Removing a camera must remove its highlight');
 await page.getByRole('button',{name:'Arrastar Câmera 2',exact:true}).click();
 await page.getByRole('group',{name:'Posição 1 do mosaico',exact:true}).press('Enter');
 await page.getByRole('button',{name:'Fechar câmera da posição 2',exact:true}).click();
 assert(await marked.count()===3,'A duplicated camera stays highlighted while another occurrence remains');
 for(const size of [1,2,3,4]){
  await page.getByRole('combobox',{name:'Layout do mosaico'}).selectOption(String(size));
  await noOverlap();
  assert(await marked.count()===(size===1?1:3),'Only cameras within the current layout must be highlighted');
 }
 await page.getByRole('combobox',{name:'Layout do mosaico'}).selectOption('2');
 await page.getByRole('button',{name:'Alternar maximização',exact:true}).click();
 await noOverlap();
 await page.getByRole('button',{name:'Alternar maximização',exact:true}).click();
 await page.getByRole('button',{name:'Recuar listagem',exact:true}).click();
 assert(!await page.locator('#live-camera-list-content').isVisible(),'The header toggle must hide the panel');
 await page.getByRole('button',{name:'Exibir listagem',exact:true}).click();
 assert(await page.locator('#live-camera-list-content').isVisible(),'The header toggle must reopen the panel');
 await page.getByRole('tab',{name:'Visualização',exact:true}).click();
 for(const name of ['Portária','Estacionamento']){
  await page.getByRole('button',{name:'Salvar mosaico atual',exact:true}).click();
  await page.getByRole('textbox',{name:'Nome da visualização'}).fill(name);
  await page.getByRole('button',{name:'Salvar',exact:true}).click();
  await page.getByRole('dialog',{name:'Salvar visualização'}).waitFor({state:'detached'});
 }
 const search=page.getByRole('searchbox',{name:'Buscar visualização',exact:true});
 await search.fill(' PORTARIA ');
 assert(await page.locator('.live-saved-view').count()===1,'Search must ignore case, whitespace and accents');
 assert(await page.locator('.live-saved-view').innerText()==='Portária','View rows must show only their names');
 assert(await page.locator('.live-saved-view svg, .live-saved-view small').count()===0,'View rows must not contain icons or metadata');
 await page.getByRole('button',{name:'Carregar visualização Portária',exact:true}).click();
 assert(await page.locator('.live-saved-views .live-camera-notice').count()===0,'Loading a view must not show a success notice');
 await search.fill('inexistente');
 await page.getByText('Nenhuma visualização encontrada.',{exact:true}).waitFor();
 await search.fill('');
 assert(await page.locator('.live-saved-view').count()===2,'Clearing search must restore all saved views');
 await page.screenshot({path:'output/playwright/live-panel-views.png'});
 await page.getByRole('tab',{name:'Câmeras',exact:true}).click();
 await page.screenshot({path:'output/playwright/live-panel-cameras.png'});
 return {passed:true,checks:['toggle avoids camera close buttons in every layout and maximization','collapse and reopen from the header','all mosaic cameras highlighted','removal and duplicate cameras','highlight follows layout changes','view search without accents or case','name-only rows','no view-loaded notice']};
}
