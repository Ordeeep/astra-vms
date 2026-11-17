import {chromium,expect} from '@playwright/test';
import {readFile,mkdir} from 'node:fs/promises';

const browser=await chromium.launch({channel:'chrome',headless:true});
const output=new URL('../../../output/playwright/camera-recording/',import.meta.url);
await mkdir(output,{recursive:true});
const checks=[];
let page;
try{
 page=await browser.newPage({viewport:{width:1440,height:1000}});
 page.setDefaultTimeout(10000);
 const errors=[];page.on('pageerror',error=>{errors.push(error.message);console.error(error.message);});
 await page.route('http://127.0.0.1:13129/**',async route=>{
  const pathname=new URL(route.request().url()).pathname;
  if(pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html lang="pt-BR" class="dark" style="--font-inter:Arial,sans-serif"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><div id="root"></div><script src="/fixture.js"></script></body></html>'});
  if(!['/fixture.js','/fixture.css'].includes(pathname))return route.abort();
  return route.fulfill({contentType:pathname.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:await readFile(new URL(`.generated${pathname}`,import.meta.url))});
 });
 const fresh=async(mode='')=>{await page.goto('http://127.0.0.1:13129/?mode='+mode);try{await expect(page.getByRole('row',{name:'Editar câmera Portaria'})).toBeVisible();}catch(error){console.log(await page.locator('body').innerText());throw error;}};
 const dialog=()=>page.getByRole('dialog');
 const tab=async name=>dialog().getByRole('tab',{name,exact:true}).click();
 const edit=async()=>page.getByRole('row',{name:'Editar câmera Portaria'}).click();
 const save=async()=>dialog().getByRole('button',{name:'Salvar',exact:true}).click();
 const mutation=()=>page.evaluate(()=>window.recordingQA.requests.filter(r=>r.method==='PATCH'||r.method==='POST'&&r.path.endsWith('/cameras')).at(-1));
 const discard=async()=>{page.once('dialog',prompt=>prompt.accept());await dialog().getByRole('button',{name:'Cancelar',exact:true}).click();await expect(dialog()).toHaveCount(0);};

 await fresh();await expect(page.getByRole('cell',{name:'Gravando',exact:true})).toBeVisible();
 await edit();await tab('Gravação');
 await expect(dialog().getByRole('checkbox',{name:'Habilitar gravação'})).toBeChecked();
 await expect(dialog().getByLabel('Perfil gravado')).toHaveValue('sub1');
 await expect(dialog().getByText('Última escrita:',{exact:false})).toBeVisible();
 await expect(dialog().getByText(/America\/Sao_Paulo/)).toBeVisible();
 await dialog().getByRole('checkbox',{name:'Gravar todos os dias, 24 horas'}).uncheck();
 await dialog().getByRole('button',{name:'+ Adicionar faixa'}).click();
 await dialog().getByLabel('Início',{exact:true}).fill('22:00');await dialog().getByLabel('Fim',{exact:true}).fill('06:00');
 await dialog().getByLabel('Retenção específica',{exact:false}).fill('7');
 await tab('Informações');await dialog().getByRole('textbox',{name:'Nome',exact:true}).fill('Portaria atualizada');await tab('Gravação');
 await expect(dialog().getByLabel('Fim',{exact:true})).toHaveValue('06:00');
 await page.screenshot({path:new URL('recording-dark.png',output).pathname.slice(1),fullPage:true});
 await save();await expect(dialog()).toHaveCount(0);
 let request=await mutation();expect(request.body.name).toBe('Portaria atualizada');expect(request.body.recording.profile).toBe('sub1');expect(request.body.recording.schedule[0]).toEqual({day:1,start:1320,end:360});
 checks.push('salvamento conjunto, agenda noturna e preservação entre abas');

 await fresh();await page.getByRole('button',{name:'+ Nova câmera'}).click();await dialog().getByRole('combobox',{name:/^Site/}).selectOption('site');await tab('Gravação');
 await expect(dialog().getByRole('checkbox',{name:'Habilitar gravação'})).not.toBeChecked();await expect(dialog().getByRole('checkbox',{name:'Gravar todos os dias, 24 horas'})).toBeChecked();
 await expect(dialog().getByLabel('Retenção específica',{exact:false})).toHaveValue('');
 await tab('Informações');await dialog().getByLabel('Nome',{exact:true}).fill('Nova câmera');await dialog().getByLabel('Endereço RTSP principal',{exact:true}).fill('rtsp://synthetic/main');await save();
 expect((await mutation()).body.recording.enabled).toBe(false);
 checks.push('criação com gravação desabilitada e retenção herdada');

 await fresh();await page.getByRole('button',{name:'+ Nova câmera'}).click();await dialog().getByRole('combobox',{name:/^Site/}).selectOption('site');
 await dialog().getByRole('combobox',{name:/^Tipo de conexão/}).selectOption('dahua');await dialog().getByLabel('Nome',{exact:true}).fill('Câmera sem perfis');await dialog().getByLabel('Endereço',{exact:true}).fill('192.0.2.10');await dialog().getByLabel('Usuário',{exact:true}).fill('fixture');await dialog().getByLabel('Senha',{exact:true}).fill('fixture-only');
 await tab('Gravação');await expect(dialog().getByRole('checkbox',{name:'Habilitar gravação'})).toBeDisabled();await expect(dialog().getByText(/Obtenha os perfis/)).toBeVisible();await save();await expect(dialog()).toHaveCount(0);expect((await mutation()).body.recording.enabled).toBe(false);checks.push('cadastro sem perfis mantém gravação desabilitada');

 await fresh();await edit();await tab('Streams');await dialog().getByRole('button',{name:'Remover substream'}).click();await save();
 await expect(dialog().getByRole('tab',{name:'Gravação',exact:true})).toHaveAttribute('data-state','active');await expect(dialog().getByRole('alert').first()).toContainText(/Escolha um perfil/);
 expect(await mutation()).toBeUndefined();await dialog().getByLabel('Perfil gravado').selectOption('main');await save();expect((await mutation()).body.recording.profile).toBe('main');
 checks.push('perfil removido exige escolha explícita');

 await fresh();await edit();await dialog().getByLabel('Nome',{exact:true}).fill('Rascunho');
 page.once('dialog',prompt=>prompt.dismiss());await dialog().getByRole('button',{name:'Cancelar',exact:true}).click();await expect(dialog()).toBeVisible();await expect(dialog().getByLabel('Nome',{exact:true})).toHaveValue('Rascunho');
 await discard();checks.push('confirmação de descarte');

 await fresh();await edit();await dialog().getByLabel('Nome',{exact:true}).fill('Conflito');await page.evaluate(()=>window.recordingQA.conflict(true));await save();
 await expect(dialog().getByRole('alert')).toContainText(/outro operador/);await expect(dialog().getByLabel('Nome',{exact:true})).toHaveValue('Conflito');await expect(dialog().getByRole('button',{name:'Salvar',exact:true})).toBeDisabled();
 await page.evaluate(()=>window.recordingQA.conflict(false));page.once('dialog',prompt=>prompt.accept());await dialog().getByRole('button',{name:'Reabrir versão atual'}).click();await expect(dialog().getByLabel('Nome',{exact:true})).toHaveValue('Portaria');
 checks.push('conflito mantém rascunho e permite reabrir');

 await fresh('recording-only');await edit();await expect(dialog().getByRole('tab',{name:'Gravação',exact:true})).toHaveAttribute('data-state','active');await expect(dialog().getByRole('tab',{name:'Streams',exact:true})).toHaveCount(0);
 await tab('Informações');await expect(dialog().getByRole('textbox')).toHaveCount(0);await tab('Gravação');await dialog().getByRole('checkbox',{name:'Habilitar gravação'}).uncheck();await save();
 request=await mutation();expect(request.body.recordingOnly).toBe(true);expect(request.body.name).toBeUndefined();expect(request.body.source).toBeUndefined();checks.push('gestão de gravação sem permissão de cadastro');

 await fresh('pending');await expect(page.getByRole('cell',{name:'Destino de armazenamento pendente'})).toBeVisible();await edit();await tab('Gravação');await expect(dialog().getByText(/O destino ou a cota/)).toBeVisible();await dialog().getByRole('button',{name:'Abrir Armazenamento'}).click();await expect(page.getByRole('heading',{name:'Armazenamento',exact:true})).toBeVisible();
 await expect(page.getByRole('heading',{name:/Política do ambiente atual/})).toBeVisible();await expect(page.getByRole('heading',{name:'Armazenamento do servidor'})).toBeVisible();
 await page.screenshot({path:new URL('storage-dark.png',output).pathname.slice(1),fullPage:true});checks.push('pendência explícita e controles de armazenamento separados');
 await expect(page.getByRole('alert')).toHaveCount(0);
 await page.getByRole('button',{name:'Cota e retenção',exact:true}).click();await dialog().getByLabel('Retenção padrão (dias)',{exact:true}).fill('14');await save();await expect(dialog()).toHaveCount(0);await expect(page.getByText('14 dias',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Configurar armazenamento',exact:true}).click();await dialog().getByLabel('Pasta no servidor',{exact:true}).fill('D:\\Astra\\Fixture');await save();await expect(dialog()).toHaveCount(0);await expect(page.getByRole('button',{name:'Configurar armazenamento',exact:true})).toHaveCount(0);checks.push('política e destino usam os controles existentes na nova área');

 await fresh();await page.evaluate(()=>window.recordingQA.failRecording(true));await edit();await expect(dialog().getByRole('button',{name:'Salvar',exact:true})).toBeDisabled();await tab('Gravação');await page.evaluate(()=>window.recordingQA.failRecording(false));await dialog().getByRole('button',{name:'Tentar carregar gravação novamente'}).click();await expect(dialog().getByLabel('Perfil gravado')).toHaveValue('sub1');await expect(dialog().getByRole('button',{name:'Salvar',exact:true})).toBeEnabled();checks.push('falha de leitura impede sobrescrever a configuração');

 await fresh();await edit();await tab('Gravação');await dialog().getByLabel('Retenção específica',{exact:false}).fill('1');await page.evaluate(()=>window.recordingQA.retentionImpact(true));await save();await expect(dialog().getByText(/1.0 MiB/)).toBeVisible();await dialog().getByRole('checkbox',{name:'Confirmo a aplicação ao histórico existente.'}).check();await save();expect((await mutation()).body.recording.confirmImpact).toBe(true);checks.push('confirmação de impacto de retenção');

 await fresh('viewer');await edit();await tab('Gravação');await expect(dialog().getByText(/Última escrita/)).toBeVisible();await expect(dialog().getByRole('checkbox')).toHaveCount(0);checks.push('consulta sem edição para operador de reprodução');

 for(const dark of [true,false])for(const width of [1440,390,320]){
  await fresh();await page.setViewportSize({width,height:900});await page.evaluate(dark=>document.documentElement.classList.toggle('dark',dark),dark);await edit();await tab('Gravação');
  const fits=await dialog().evaluate(element=>{const box=element.getBoundingClientRect();return box.left>=0&&box.right<=innerWidth+1&&box.top>=0&&box.bottom<=innerHeight+1&&element.scrollWidth<=element.clientWidth+1;});
  expect(fits).toBe(true);await page.screenshot({path:new URL(`recording-${dark?'dark':'light'}-${width}.png`,output).pathname.slice(1),fullPage:true});await dialog().getByRole('button',{name:'Cancelar',exact:true}).click();
 }
 checks.push('layout em 1440, 390 e 320 px, temas claro e escuro');
 expect(errors).toEqual([]);console.log(JSON.stringify({checks},null,2));
}catch(error){if(page){console.log(await page.locator('body').innerText());await page.screenshot({path:new URL('failure.png',output).pathname.slice(1),fullPage:true});}throw error;}finally{await browser.close();}
