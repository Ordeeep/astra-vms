import {chromium} from '@playwright/test';
import {readFile, mkdir} from 'node:fs/promises';

// Serve the bundled fixture through Playwright: no backend or listening port.
const browser=await chromium.launch({channel:'chrome',headless:true});
try {
 const page=await browser.newPage({viewport:{width:1440,height:900}});
 await page.route('http://127.0.0.1:13110/**',async route=>{
  const name=new URL(route.request().url()).pathname;
  if(name==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html lang="pt-BR"><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><script src="/fixture.js"></script></body></html>'});
  if(/^\/mosaic-layouts\/[a-z\d-]+\.png$/.test(name))return route.fulfill({contentType:'image/png',body:await readFile(new URL(`../../public${name}`,import.meta.url))});
  if(!['/fixture.js','/fixture.css'].includes(name))return route.abort();
  return route.fulfill({contentType:name.endsWith('.css')?'text/css; charset=utf-8':'text/javascript; charset=utf-8',body:await readFile(new URL(`.generated${name}`,import.meta.url))});
 });
 await mkdir('output/playwright/live-continuity',{recursive:true});
 if(process.argv.includes('--native'))await page.goto('http://127.0.0.1:13110/#native');
 const script=process.argv.slice(2).find(arg=>!arg.startsWith('--'))??'stream-release.cjs';
 const check=eval(`(${await readFile(new URL(script,import.meta.url),'utf8')})`);
 console.log(JSON.stringify(await check(page),null,2));
} finally {
 await browser.close();
}
