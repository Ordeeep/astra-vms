"use client";
import {CircleCheck,CircleHelp,CirclePause,CircleX,TriangleAlert} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Popover,PopoverContent,PopoverTrigger} from "@/components/ui/popover";
import {availabilityLabels,availabilityReasons,cameraAvailability,type AvailabilityState} from "@/lib/camera-availability";
import {type Camera} from "@/components/management";

const icons={online:CircleCheck,offline:CircleX,unknown:CircleHelp,suspended:CirclePause};
const colors:Record<AvailabilityState,string>={online:"text-emerald-700 dark:text-emerald-400",offline:"text-red-700 dark:text-red-400",unknown:"text-muted-foreground",suspended:"text-muted-foreground"};
function time(value?:string){return value&&Number.isFinite(Date.parse(value))?new Date(value).toLocaleString("pt-BR"):"Ainda não houve confirmação";}
export function CameraAvailabilityBadge({camera,now}:{camera:Camera;now:number}){
 const status=cameraAvailability(camera,now),Icon=icons[status.state],warnings=status.warnings??[];
 const reason=status.code?availabilityReasons[status.code]??"Não foi possível determinar a causa da falha.":status.state==="online"?"Vídeo recebido em pelo menos um perfil.":"Aguardando uma verificação válida.";
 return <Popover><PopoverTrigger asChild><Button type="button" variant="ghost" size="sm" className="h-auto justify-start gap-2 px-1 text-xs" aria-label={`Disponibilidade de ${camera.name}: ${availabilityLabels[status.state]}`}>
  <Icon aria-hidden="true" className={`size-4 shrink-0 ${colors[status.state]}`}/><span>{availabilityLabels[status.state]}</span>
  {warnings.length>0&&<TriangleAlert aria-label="Há falhas de perfil conhecidas" className="size-3.5 text-amber-700 dark:text-amber-400"/>}
 </Button></PopoverTrigger><PopoverContent align="start" className="w-80 max-w-[calc(100vw-2rem)] gap-3 p-4" aria-label={`Disponibilidade de ${camera.name}`}>
  <strong className="text-sm">{availabilityLabels[status.state]}</strong><span>{reason}</span>
  {status.profile&&<span>Perfil com vídeo: {camera.profiles.find(p=>p.id===status.profile)?.name??status.profile}</span>}
  <dl className="grid gap-2 text-xs"><div><dt className="text-muted-foreground">Última verificação</dt><dd>{status.checkedAt?time(status.checkedAt):"Ainda não verificada"}</dd></div><div><dt className="text-muted-foreground">Último vídeo confirmado</dt><dd>{time(status.lastVideoAt)}</dd></div></dl>
  {warnings.length>0&&<div className="grid gap-2 border-t pt-3"><strong>Falhas de perfil conhecidas</strong>{warnings.map(w=><div key={w.profile}><span>{camera.profiles.find(p=>p.id===w.profile)?.name??w.profile}: {availabilityReasons[w.code??""]??"Não foi possível receber vídeo."}</span><small className="text-muted-foreground">{time(w.checkedAt)}</small></div>)}</div>}
 </PopoverContent></Popover>;
}
export function CameraAvailabilityCounts({cameras,now}:{cameras:Camera[];now:number}){
 const counts:Record<AvailabilityState,number>={online:0,offline:0,unknown:0,suspended:0};
 for(const camera of cameras)counts[cameraAvailability(camera,now).state]++;
 return <div className="mb-5 flex flex-wrap gap-x-5 gap-y-2 text-xs" aria-label="Resumo de disponibilidade das câmeras listadas">{(Object.keys(counts) as AvailabilityState[]).map(state=>{const Icon=icons[state];return <span key={state} className="inline-flex items-center gap-1.5"><Icon aria-hidden="true" className={`size-3.5 ${colors[state]}`}/>{availabilityLabels[state]}: <strong>{counts[state]}</strong></span>;})}</div>;
}
