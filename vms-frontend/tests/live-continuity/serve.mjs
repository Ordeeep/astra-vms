import http from 'node:http';
import fs from 'node:fs';

// Test fixture only; no production routes or backend connections.
http.createServer((request,response)=>{
 if(request.url==='/'){
  response.setHeader('Content-Type','text/html; charset=utf-8');
  response.end('<!doctype html><html lang="pt-BR"><head><link rel="stylesheet" href="/fixture.css"></head><body><script src="/fixture.js"></script></body></html>');return;
 }
 const name={'/fixture.js':'fixture.js','/fixture.css':'fixture.css'}[request.url];
 if(!name){response.writeHead(404);response.end();return;}
 response.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');
 fs.createReadStream(new URL(`.generated/${name}`,import.meta.url)).pipe(response);
}).listen(13110,'127.0.0.1',()=>console.log('Live continuity fixture: http://127.0.0.1:13110'));
