"use client";

import {Label} from "@/components/ui/label";
import {Checkbox} from "@/components/ui/checkbox";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {NativeSelect,NativeSelectOption} from "@/components/ui/native-select";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {type Profile} from "@/components/management";
import {type RecordingConfig,type RecordingStatus,recordingLabels} from "@/lib/camera-recording";

const days=["Domingo","Segunda","Terça","Quarta","Quinta","Sexta","Sábado"];
const time=(n:number)=>`${String(Math.floor(n/60)).padStart(2,"0")}:${String(n%60).padStart(2,"0")}`;
const minute=(value:string)=>value?Number(value.split(":")[0])*60+Number(value.split(":")[1]):NaN;

type Props={config:RecordingConfig;onChange:(config:RecordingConfig)=>void;profiles:Profile[];timezone:string;status?:RecordingStatus;storagePending:boolean;canManageStorage:boolean;onStorage:()=>void;impact:number|null;confirmImpact:boolean;onConfirmImpact:(checked:boolean)=>void};

export function CameraRecordingEditor({config,onChange,profiles,timezone,status,storagePending,canManageStorage,onStorage,impact,confirmImpact,onConfirmImpact}:Props){
 const selectable=profiles.filter(p=>p.enabled!==false);
 const selected=selectable.some(p=>p.id===config.profile);
 return <>
  <h3>Gravação</h3>
  <p className="muted-text">Configure a captura desta câmera. Horário local do site: {timezone}.</p>
  {status&&<div className="recording-camera-state"><p><strong>Estado atual:</strong> {recordingLabels[status.code]??status.code}</p><p><strong>Última escrita:</strong> {status.lastWrite?new Date(status.lastWrite).toLocaleString():"Nenhuma escrita informada"}</p></div>}
  <Label className="check"><Checkbox checked={config.enabled} disabled={!config.enabled&&!selectable.length} onCheckedChange={value=>onChange({...config,enabled:value===true})}/>Habilitar gravação</Label>
  {!selectable.length&&<Alert role="note"><AlertDescription>Obtenha os perfis em Informações ou Streams para habilitar a gravação. Você pode salvar o cadastro com a gravação desabilitada.</AlertDescription></Alert>}
  {config.enabled&&!selected&&<Alert role="alert" variant="destructive"><AlertDescription>O perfil gravado não está disponível. Escolha outro perfil ou desabilite a gravação antes de salvar.</AlertDescription></Alert>}
  {storagePending&&<Alert role="note"><AlertDescription>O destino ou a cota de armazenamento ainda não está configurado. Você pode salvar a configuração, mas a captura depende de resolver essa pendência.{canManageStorage&&<Button type="button" variant="link" onClick={onStorage}>Abrir Armazenamento</Button>}</AlertDescription></Alert>}
  <Label className="field-label">Perfil gravado<NativeSelect value={selected?config.profile:""} disabled={!selectable.length} required={config.enabled} onChange={event=>onChange({...config,profile:event.target.value})} className="w-full">
   {!selected&&<NativeSelectOption value="" disabled>Selecione um perfil</NativeSelectOption>}
   {selectable.map(profile=><NativeSelectOption key={profile.id} value={profile.id}>{profile.name} · {profile.id}</NativeSelectOption>)}
  </NativeSelect></Label>
  <RecordingScheduleEditor always={config.always} schedule={config.schedule} onChange={value=>onChange({...config,...value})}/>
  <Label className="field-label">Retenção específica (dias; vazio herda o padrão)<Input type="number" min={1} max={3650} value={config.retentionDays??""} onChange={event=>onChange({...config,retentionDays:event.target.value?Number(event.target.value):null})}/></Label>
  <p className="muted-text">A retenção depende da capacidade disponível. Se faltar espaço, gravações antigas podem ser sobrescritas antes do prazo.</p>
  <Alert role="note"><AlertDescription>Se o perfil escolhido falhar, a gravação registra uma lacuna e aguarda sua recuperação. A qualidade da visualização ao vivo é independente.</AlertDescription></Alert>
  {impact!==null&&<Alert role="note"><AlertDescription><p>A nova retenção torna {(impact/1024**2).toFixed(1)} MiB do acervo elegíveis para descarte.</p><Label className="check"><Checkbox checked={confirmImpact} onCheckedChange={value=>onConfirmImpact(value===true)}/>Confirmo a aplicação ao histórico existente.</Label></AlertDescription></Alert>}
 </>;
}

export type RecordingSchedule = Pick<RecordingConfig,"always"|"schedule">;

export function RecordingScheduleEditor({always,schedule,onChange}:RecordingSchedule&{onChange:(value:RecordingSchedule)=>void}) {
 return <>
  <Label className="check"><Checkbox checked={always} onCheckedChange={value=>onChange({always:value===true,schedule})}/>Gravar todos os dias, 24 horas</Label>
  {!always&&<>
   <div className="recording-schedule-rows">{schedule.map((window,index)=>{
    const fullDay=window.start===0&&window.end===1440;
    const update=(patch:Partial<typeof window>)=>onChange({always,schedule:schedule.map((value,n)=>n===index?{...value,...patch}:value)});
    return <div className="recording-schedule-row" key={index}>
     <Label className="field-label">Dia<NativeSelect value={window.day} onChange={event=>update({day:Number(event.target.value)})}>{days.map((day,n)=><NativeSelectOption key={n} value={n}>{day}</NativeSelectOption>)}</NativeSelect></Label>
     <Label className="check"><Checkbox checked={fullDay} onCheckedChange={value=>update(value===true?{start:0,end:1440}:{start:480,end:1080})}/>Dia inteiro</Label>
     <Label className="field-label">Início<Input type="time" required disabled={fullDay} value={time(window.start)} onChange={event=>update({start:minute(event.target.value)})}/></Label>
     <Label className="field-label">Fim<Input type="time" required disabled={fullDay} value={time(window.end%1440)} onChange={event=>update({end:minute(event.target.value)})}/></Label>
     <Button type="button" variant="ghost" onClick={()=>onChange({always,schedule:schedule.filter((_,n)=>n!==index)})}>Remover faixa {index+1}</Button>
    </div>;
   })}</div>
   <Button type="button" variant="outline" disabled={schedule.length>=56} onClick={()=>onChange({always,schedule:[...schedule,{day:1,start:480,end:1080}]})}>+ Adicionar faixa</Button>
   <small>Uma faixa cujo fim vem antes do início termina no dia seguinte.{!schedule.length&&" Sem faixas, a câmera fica fora da agenda."}</small>
  </>}
 </>;
}
