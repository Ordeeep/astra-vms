"use client";

import {useEffect,useRef,useState,type ReactNode} from "react";
import {Button} from "@/components/ui/button";
import {Label} from "@/components/ui/label";
import {Input} from "@/components/ui/input";
import {NativeSelect,NativeSelectOption} from "@/components/ui/native-select";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {RTSPConnectionTest} from "@/components/rtsp-connection-test";
import {api} from "@/lib/api";
import {type Profile} from "@/components/management";

export type ConnectionType="rtsp"|"dahua"|"hikvision";
export type CameraConnection={host:string;protocol:"http"|"https";httpPort:number;username:string;password?:string;hasPassword?:boolean;profileStatus?:"pending"|"known"|"discovered"};
export const emptyCameraConnection=():CameraConnection=>({host:"",protocol:"http",httpPort:80,username:"",password:""});
export const savedCameraConnection=(saved?:CameraConnection):CameraConnection=>saved?{host:saved.host,protocol:saved.protocol,httpPort:saved.httpPort,username:saved.username,hasPassword:saved.hasPassword,profileStatus:saved.profileStatus,password:""}:emptyCameraConnection();
type Device={model?:string;serial?:string;vendor?:string;firmware?:string};
type Video={codec:string;width:number;height:number;fps:number};
type Discovery={device?:Device;profiles:Profile[];connection?:Pick<CameraConnection,"profileStatus">;api:{status:"ok"|"error";code?:string}};
type Props={tenant:string;siteId:string;cameraId?:string;maintenance:boolean;savedProfileIds?:string[];connectionType:ConnectionType;onTypeChange:(type:ConnectionType)=>void;connection:CameraConnection;onConnectionChange:(connection:CameraConnection)=>void;profiles:Profile[];onProfilesChange:(profiles:Profile[])=>void;render:(fields:ReactNode,streams:ReactNode)=>ReactNode};
const apiErrors:Record<string,string>={camera_auth_failed:"A API recusou as credenciais. Verifique o usuário e a senha.",camera_api_timeout:"A consulta à API excedeu o prazo. Verifique a conexão com a câmera.",camera_api_unavailable:"A API não respondeu. Verifique o endereço e a porta.",camera_api_unsupported:"Este equipamento não disponibilizou a consulta solicitada.",camera_api_response_invalid:"A câmera devolveu uma resposta que não pôde ser reconhecida.",camera_auth_unsupported:"O método de autenticação da câmera ainda não é compatível.",camera_auth_challenge_invalid:"A câmera devolveu um desafio de autenticação inválido.",camera_auth_unavailable:"Não foi possível concluir a autenticação com a câmera."};

export function CameraConnectionEditor({tenant,siteId,cameraId,maintenance,savedProfileIds=[],connectionType,onTypeChange,connection,onConnectionChange,profiles,onProfilesChange,render}:Props){
 const [discovery,setDiscovery]=useState<Discovery|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(""),[testing,setTesting]=useState<string|null>(null),[videos,setVideos]=useState<Record<string,Video>>({}),[videoErrors,setVideoErrors]=useState<Record<string,string>>({});
 const [editingPassword,setEditingPassword]=useState(false);
 // The saved-secret mask is only presentation; connection.password contains
 // exclusively a new value entered by the operator, never the display mask.
 const showSavedPassword=maintenance&&connection.hasPassword&&!connection.password&&!editingPassword;
 const generation=useRef(0);
 // A result belongs to both the access details and the selected profiles.
 const connectionFingerprint=JSON.stringify({tenant,siteId,cameraId,connectionType,connection});
 const profileFingerprint=JSON.stringify(profiles.map(p=>({id:p.id,url:p.url})));
 useEffect(()=>{generation.current++;setDiscovery(null);setBusy(false);setError("");setTesting(null);setVideos({});setVideoErrors({});return()=>{generation.current++;};},[connectionFingerprint]);
 useEffect(()=>{generation.current++;setBusy(false);setTesting(null);setVideos({});setVideoErrors({});return()=>{generation.current++;};},[profileFingerprint]);
 const allowed=!!(cameraId||siteId),hasCredentials=!!connection.username.trim()&&(!!connection.password||!!(maintenance&&connection.hasPassword));
 const canQuery=allowed&&!!connection.host.trim()&&hasCredentials&&connection.httpPort>=1&&connection.httpPort<=65535;
 const input=()=>({siteId,...(cameraId?{cameraId}:{}),connectionType,connection:{host:connection.host.trim(),protocol:connection.protocol,httpPort:connection.httpPort,username:connection.username,...(connection.password?{password:connection.password}:{})}});
 const change=(patch:Partial<CameraConnection>)=>{
  generation.current++;
  onConnectionChange({...connection,...patch});
  onProfilesChange(maintenance?profiles:[]);
 };
 async function discover(){
  const request=++generation.current;setBusy(true);setError("");
  try{
   const result=await api<Discovery>(`/tenants/${tenant}/source-discovery`,"POST",input(),true);
   if(request!==generation.current)return;
   if(result.api.status==="ok"||maintenance)onProfilesChange(result.profiles.map(p=>({...p,url:undefined})));
   setDiscovery(result);
  }catch(e){if(request===generation.current)setError((e as Error).message);}
  finally{if(request===generation.current)setBusy(false);}
 }
 async function test(profile:string){
  const request=++generation.current;setTesting(profile);setVideoErrors(current=>({...current,[profile]:""}));
  try{const result=await api<{video:Video}>(`/tenants/${tenant}/source-tests`,"POST",{...input(),profile},true);if(request===generation.current)setVideos(current=>({...current,[profile]:result.video}));}
  catch(e){if(request===generation.current)setVideoErrors(current=>({...current,[profile]:(e as Error).message}));}
  finally{if(request===generation.current)setTesting(null);}
 }
 const rtspProfile=(p:Profile)=><div className="camera-profile" role="group" aria-label={p.id==="main"?"Perfil principal":p.name} key={p.id}><Label className="field-label">{p.id==="main"?"Endereço RTSP principal":`Endereço RTSP · ${p.name}`}<Input required={!maintenance||!savedProfileIds.includes(p.id)} type="text" autoComplete="off" spellCheck={false} placeholder={maintenance&&savedProfileIds.includes(p.id)?"Deixe em branco para manter o endereço cadastrado":"rtsp://usuario:senha@endereco:554/caminho"} value={p.url??""} onChange={event=>onProfilesChange(profiles.map(v=>v.id===p.id?{...v,url:event.target.value}:v))}/></Label><RTSPConnectionTest tenant={tenant} siteId={siteId} cameraId={cameraId} url={p.url??""}/>{p.id!=="main"&&<Button type="button" onClick={()=>onProfilesChange(profiles.filter(v=>v.id!==p.id))} variant="ghost">Remover substream</Button>}</div>;
 const discoveryControls=<>
  <Button type="button" variant="outline" disabled={busy||testing!==null||!canQuery} onClick={()=>void discover()}>{busy?"Consultando câmera…":"Consultar câmera e perfis"}</Button>
  {!allowed&&<small>Selecione um site para consultar a câmera.</small>}
  {error&&<Alert variant="destructive" role="alert"><AlertDescription>{error}</AlertDescription></Alert>}
  {discovery?.api.status==="ok"&&<Alert role="status"><AlertDescription>API autenticada{discovery.device?.model?` · ${discovery.device.model}`:""}{discovery.device?.vendor?` · ${discovery.device.vendor}`:""}{discovery.device?.firmware?` · ${discovery.device.firmware}`:""}. O acesso ao vídeo é verificado separadamente.{discovery.device?.serial&&<small className="block">Serial: {discovery.device.serial}</small>}</AlertDescription></Alert>}
  {discovery?.api.status==="error"&&<Alert variant="destructive" role="alert"><AlertDescription>{apiErrors[discovery.api.code??""]??"Não foi possível consultar a API da câmera. Verifique conexão, credenciais e compatibilidade do equipamento."} Você pode salvar o cadastro e testar novamente depois.</AlertDescription></Alert>}
 </>;
 const fields=<>
  <div className="vendor-connection-grid connection-type-grid">
  <Label className="field-label">Tipo de conexão<NativeSelect name="connectionType" value={connectionType} onChange={event=>onTypeChange(event.target.value as ConnectionType)} className="w-full"><NativeSelectOption value="rtsp">RTSP</NativeSelectOption><NativeSelectOption value="dahua">Dahua</NativeSelectOption><NativeSelectOption value="hikvision">Hikvision</NativeSelectOption>{["ONVIF","Axis","Intelbras"].map(type=><NativeSelectOption key={type} value={type.toLowerCase()} disabled>{type} · Não implementado</NativeSelectOption>)}</NativeSelect></Label>
   {connectionType!=="rtsp"&&<Label className="field-label">Protocolo<NativeSelect className="w-full" value={connection.protocol} onChange={event=>change({protocol:event.target.value as "http"|"https",httpPort:connection.httpPort===(connection.protocol==="http"?80:443)?(event.target.value==="https"?443:80):connection.httpPort})}><NativeSelectOption value="http">HTTP</NativeSelectOption><NativeSelectOption value="https">HTTPS</NativeSelectOption></NativeSelect></Label>}
  </div>
  {connectionType!=="rtsp"&&
   <div className="vendor-connection-grid">
    <Label className="field-label">Endereço<Input required autoComplete="off" spellCheck={false} maxLength={253} placeholder="IP ou domínio" value={connection.host} onChange={event=>change({host:event.target.value})}/></Label>
    <Label className="field-label">Porta da câmera<Input required type="number" min={1} max={65535} value={connection.httpPort||""} onChange={event=>change({httpPort:Number(event.target.value)})}/></Label>
    <Label className="field-label">Usuário<Input required autoComplete="off" maxLength={256} value={connection.username} onChange={event=>change({username:event.target.value})}/></Label>
    <Label className="field-label">Senha<Input required={!maintenance||!connection.hasPassword} type="password" autoComplete="new-password" maxLength={1024} value={showSavedPassword?"*********":connection.password??""} onFocus={()=>setEditingPassword(true)} onBlur={()=>setEditingPassword(false)} onChange={event=>change({password:event.target.value})}/></Label>
   </div>
  }
  {connectionType==="rtsp"?<div className="profile-editor">{profiles.filter(p=>p.id==="main").map(rtspProfile)}</div>:<div className="camera-discovery-controls">{discoveryControls}</div>}
 </>;
 const streams=<>
  {connectionType==="rtsp"?<>
   <p className="muted-text">Substreams opcionais. O endereço principal fica em Informações.</p>
   <div className="profile-editor">{profiles.filter(p=>p.id!=="main").map(rtspProfile)}</div>
   {profiles.length<5&&<Button type="button" onClick={()=>{const id=["sub1","sub2","sub3","sub4"].find(id=>!profiles.some(p=>p.id===id));if(id)onProfilesChange([...profiles,{id,name:`Substream ${id.slice(3)}`,url:""}]);}} variant="outline">+ Adicionar substream</Button>}
  </>:<>
   <div className="camera-discovery-controls">{discoveryControls}</div>
   {maintenance&&profiles.length>0&&discovery?.api.status!=="ok"&&<p className="muted-text">Perfis identificados anteriormente. Consulte a câmera para atualizar.</p>}
   <div className="profile-editor">{profiles.map(p=><div className="camera-profile" role="group" aria-label={`Perfil ${p.name}`} key={p.id}><strong>{p.name}</strong>{p.codec&&<small>{p.codec.toUpperCase()}{p.width&&p.height?` · ${p.width} × ${p.height}`:""}{p.fps?` · ${p.fps} quadros/s`:""}{p.enabled===false?" · Desabilitado na câmera":""}</small>}<Button type="button" variant="outline" disabled={busy||testing!==null||!canQuery} onClick={()=>void test(p.id)}>{testing===p.id?"Testando vídeo…":`Testar vídeo · ${p.name}`}</Button>{videos[p.id]&&<small className="text-emerald-400" role="status">Vídeo acessível · {videos[p.id].codec.toUpperCase()} · {videos[p.id].width} × {videos[p.id].height} · {videos[p.id].fps.toFixed(2)} quadros/s</small>}{videoErrors[p.id]&&<small className="text-destructive" role="alert">{videoErrors[p.id]}</small>}</div>)}</div>
   {!profiles.length&&<p role="status" className="muted-text">{discovery?.api.status==="ok"?"A câmera não informou perfis de vídeo disponíveis.":"Os perfis serão identificados quando houver conexão."}</p>}
  </>}
 </>;
 return render(fields,streams);
}
