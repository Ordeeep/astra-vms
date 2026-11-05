"use client";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { AlertDescription, Alert } from "@/components/ui/alert";
import {useEffect,useState} from "react";
import {api} from "@/lib/api";
import type {Camera} from "@/components/management";
type Job={id:string;status:string;result?:{code?:string;video?:{codec:string;profile:string;width:number;height:number;fps:number;bFrames:number;h264Candidate:boolean}}};
export function CameraDiagnostic({tenant,camera}:{tenant:string;camera:Camera}){
 const [profile,setProfile]=useState(camera.profiles[0]?.id??"main"),[job,setJob]=useState<Job|null>(null),[error,setError]=useState(""),[busy,setBusy]=useState(false);
 const base=`/tenants/${tenant}`;
 useEffect(()=>{
  if(!job||job.status!=="running")return;
  let active=true;
  const timer=setInterval(()=>{void api<Job>(base+`/jobs/${job.id}`,"GET",undefined,true).then(value=>{if(active)setJob(value);}).catch(e=>{if(active)setError((e as Error).message);});},750);
  return()=>{active=false;clearInterval(timer);};
 },[job?.id,job?.status,base]);
 async function test(){setBusy(true);setError("");setJob(null);try{setJob(await api<Job>(base+`/cameras/${camera.id}/profile-tests`,"POST",{profile},true));}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 const v=job?.result?.video;
 return <section aria-label={`Diagnóstico · ${camera.name}`}><div className="form-grid"><Label className="field-label">Perfil de vídeo<NativeSelect value={profile} onChange={e=>setProfile(e.target.value)} className="w-full">{camera.profiles.map(p=><NativeSelectOption key={p.id} value={p.id}>{p.name}</NativeSelectOption>)}</NativeSelect></Label><div className="form-actions"><Button type="button" onClick={()=>void test()} disabled={busy||job?.status==="running"} variant="default">{busy||job?.status==="running"?"Verificando fonte…":"Testar conexão"}</Button></div></div>{error&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{job?.status==="failed"&&<Alert className="alert" role="alert" variant="destructive"><AlertDescription>Não foi possível consultar o vídeo. Verifique o endereço, as credenciais e a conexão com a câmera.</AlertDescription></Alert>}{v&&<Alert className="success" role="status"><AlertDescription><strong>Fonte de vídeo acessível</strong><p>{v.codec.toUpperCase()} · {v.width} × {v.height} · {v.fps.toFixed(2)} quadros/s · {v.profile}</p><span>{v.h264Candidate?"Perfil H.264 compatível com a base de testes. Confirme a reprodução ao vivo no navegador.":"Este perfil precisa de avaliação adicional para reprodução no navegador."}</span></AlertDescription></Alert>}</section>;
}
