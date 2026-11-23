import http from 'node:http';
import fs from 'node:fs';

// Test fixture only; no production routes or backend connections.
http.createServer((request,response)=>{
 if(request.url==='/'){
  response.setHeader('Content-Type','text/html; charset=utf-8');
  response.end('<!doctype html><html lang="pt-BR"><head><link rel="stylesheet" href="/fixture.css"></head><body><script src="/fixture.js"></script></body></html>');return;
 }
 if(/^\/mosaic-layouts\/[a-z\d-]+\.png$/.test(request.url)){
  fs.readFile(new URL(`../../public${request.url}`,import.meta.url),(error,icon)=>{
   if(error){response.writeHead(404);response.end();return;}
   response.writeHead(200,{'Content-Type':'image/png'}).end(icon);
  });return;
 }
 const name={'/fixture.js':'fixture.js','/fixture.css':'fixture.css'}[request.url];
 if(!name){response.writeHead(404);response.end();return;}
 response.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');
 fs.createReadStream(new URL(`.generated/${name}`,import.meta.url)).pipe(response);
}).listen(13110,'127.0.0.1',()=>console.log('Live continuity fixture: http://127.0.0.1:13110'));
