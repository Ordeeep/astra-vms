import React, {useState} from "react";
import {createRoot} from "react-dom/client";
import {Administration} from "@/components/management";
import {setContext, type Identity, type Tenant} from "@/lib/api";
import {type Grant} from "@/lib/permission-grants";

const tenant: Tenant = {id:"tenant-synthetic", name:"Tenant sintético", active:true, revision:1};
const identity: Identity = {csrfToken:"synthetic", tenants:[tenant], user:{id:"operator-synthetic", username:"operador.sintetico", globalAdmin:true, active:true, revision:1}};
const sites = new URL(location.href).searchParams.has("many-sites")
  ? Array.from({length:180},(_,index)=>({id:`site-${index}`,name:`Site ${String(index+1).padStart(3,"0")} — Unidade de monitoramento`}))
  : [{id:"matriz",name:"Site Matriz"},{id:"filial",name:"Site Filial"},{id:"sao-jose",name:"Site São José"}];
let groups = [{id:"group-synthetic",name:"Equipe de monitoramento",revision:7,grants:[
  {action:"live",scope:"site",resourceId:"matriz"},
  {action:"playback",scope:"site",resourceId:"matriz"},
  {action:"export",scope:"camera",resourceId:"recepcao"},
  {action:"export",scope:"camera",resourceId:"portaria"},
] as Grant[]}];
let failSave = false;
let failCatalog = new URL(location.href).searchParams.has("catalog-error");
let saveCount = 0;
const requests: string[] = [];
const json = (value:unknown, status=200) => new Response(JSON.stringify(value), {status,headers:{"Content-Type":"application/json","X-VMS-Contract":"1"}});
setContext("synthetic-context");
// All requests are handled in-memory, including writes. Nothing reaches the installation.
window.fetch = async(input,options) => {
  const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.origin);
  const route = url.pathname.replace(/^\/api\/v1/, "");
  const method = options?.method ?? "GET";
  requests.push(`${method} ${route}${url.search}`);
  const log = document.querySelector("#request-log");
  if (log) log.textContent = requests.join("\n");
  if (method === "GET" && route.endsWith("/groups")) return json({items:groups});
  if (method === "GET" && route.endsWith("/sites")) {
    if (failCatalog) {failCatalog = false; return json({code:"database_unavailable"},503);}
    const items = sites;
    // Paginate the catalog to exercise allPages rather than testing a first-page-only editor.
    const offset = Number(url.searchParams.get("cursor") ?? 0);
    return json({items:items.slice(offset,offset+2),nextCursor:offset+2<items.length ? String(offset+2) : ""});
  }
  if ((method === "PUT" || method === "POST") && /\/groups(?:\/[^/]+)?$/.test(route)) {
    if (failSave) return json({code:"revision_conflict"},412);
    const body = JSON.parse(String(options?.body));
    saveCount++;
    document.querySelector("#last-save")!.textContent = JSON.stringify({method,route,body,saveCount});
    const group = {...body,id:method === "POST" ? `new-${saveCount}` : "group-synthetic",revision:(body.revision??0)+1};
    groups = method === "POST" ? [...groups,group] : groups.map(item => item.id === group.id ? group : item);
    return json(group,method === "POST" ? 201 : 200);
  }
  return json({code:"synthetic_route_not_implemented",method,route},404);
};
function Fixture() {
  const [failing,setFailing] = useState(false);
  return <main className="astra-interface" style={{maxWidth:1280,margin:"32px auto",padding:28}}>
    <p>Validação sintética — nenhuma alteração na instalação.</p>
    <button type="button" onClick={()=>{failSave=!failSave;setFailing(failSave);}}>Falha ao salvar: {failing ? "ativa" : "inativa"}</button>
    <button type="button" onClick={()=>document.documentElement.classList.toggle("dark")}>Alternar tema</button>
    <Administration identity={identity} tenant={tenant} section="groups" onRefresh={async()=>{}}/>
    <pre id="request-log" aria-label="Chamadas simuladas"/>
    <pre id="last-save" aria-label="Última gravação simulada"/>
  </main>;
}
createRoot(document.getElementById("root")!).render(<Fixture/>);
