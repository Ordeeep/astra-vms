"use client";
import {useEffect,useRef,useState} from "react";
import {Button} from "@/components/ui/button";
import {api} from "@/lib/api";

type Video={codec:string;width:number;height:number;fps:number};
export function RTSPConnectionTest({tenant,siteId,cameraId,url}:{tenant:string;siteId:string;cameraId?:string;url:string}){
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[video,setVideo]=useState<Video|null>(null);
 const generation=useRef(0);
 useEffect(()=>{generation.current++;setBusy(false);setError("");setVideo(null);return()=>{generation.current++;};},[tenant,siteId,cameraId,url]);
 async function test(){
  const request=++generation.current;setBusy(true);setError("");setVideo(null);
  try{const result=await api<{video:Video}>(`/tenants/${tenant}/source-tests`,"POST",{url,siteId,...(cameraId?{cameraId}:{})},true);if(request===generation.current)setVideo(result.video);}
  catch(e){if(request===generation.current)setError((e as Error).message);}
  finally{if(request===generation.current)setBusy(false);}
 }
 return <div className="rtsp-connection-test"><Button type="button" variant="outline" disabled={busy||!url.trim()||(!cameraId&&!siteId)} onClick={()=>void test()}>{busy?"Testando conexão…":"Testar conexão"}</Button>{!cameraId&&!siteId&&<small>Selecione um site para testar a conexão.</small>}{video&&<small role="status" className="text-emerald-400">Conexão estabelecida · {video.codec.toUpperCase()} · {video.width} × {video.height} · {video.fps.toFixed(2)} quadros/s</small>}{error&&<small role="alert" className="text-destructive">{error}</small>}</div>;
}
