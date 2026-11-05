import http from "node:http";
import {readFile} from "node:fs/promises";
const routes={"/fixture.js":["fixture.js","text/javascript; charset=utf-8"],"/fixture.css":["fixture.css","text/css; charset=utf-8"]};
http.createServer(async(request,response)=>{
 const pathname=new URL(request.url,"http://127.0.0.1").pathname;
 response.setHeader("Cache-Control","no-store");
 if(pathname==="/favicon.ico"){response.writeHead(204);response.end();return;}
 if(pathname==="/"){
  response.setHeader("Content-Type","text/html; charset=utf-8");
  response.end(`<!doctype html><html lang="pt-BR" style="--font-inter:Arial,sans-serif"><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Live PTZ fixture</title><link rel="stylesheet" href="/fixture.css"></head><body><button id="theme-toggle" type="button" aria-label="Alternar tema" style="position:absolute;right:16px;top:8px;padding:5px 10px;border:1px solid var(--border);border-radius:6px;background:var(--background);color:var(--foreground);font:inherit;cursor:pointer">Tema escuro</button><div id="root"></div><script>
const themeButton=document.getElementById("theme-toggle");
function updateTheme(dark){document.documentElement.classList.toggle("dark",dark);document.documentElement.style.colorScheme=dark?"dark":"light";themeButton.textContent=dark?"Tema claro":"Tema escuro";themeButton.setAttribute("aria-pressed",String(dark));}
updateTheme(!new URL(location.href).searchParams.has("light"));
themeButton.addEventListener("click",()=>updateTheme(!document.documentElement.classList.contains("dark")));
</script><script src="/fixture.js"></script></body></html>`);return;
 }
 const entry=routes[pathname];
 if(!entry){response.writeHead(404);response.end();return;}
 try{const bytes=await readFile(new URL(`.generated/${entry[0]}`,import.meta.url));response.setHeader("Content-Type",entry[1]);response.end(bytes);}
 catch{response.writeHead(503);response.end("Run node tests/live-ptz/build.mjs first.");}
}).listen(13125,"127.0.0.1",()=>console.log("Live PTZ fixture: http://127.0.0.1:13125"));
