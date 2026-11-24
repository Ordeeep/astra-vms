"use client";

import {useEffect,useRef,useState,type ReactNode} from "react";
import {Power,PowerOff,Settings,Trash2,X} from "lucide-react";
import {RecordingScheduleEditor,type RecordingSchedule} from "@/components/camera-recording-editor";
import {ConfirmationDialog,EditorDialog,FormDialog} from "@/components/form-dialog";
import {cameraDisplayModes,type Camera,type CameraDisplayMode,type Site} from "@/components/management";
import {Alert,AlertDescription,AlertTitle} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Checkbox} from "@/components/ui/checkbox";
import {DropdownMenu,DropdownMenuContent,DropdownMenuItem,DropdownMenuTrigger} from "@/components/ui/dropdown-menu";
import {Label} from "@/components/ui/label";
import {NativeSelect,NativeSelectOption} from "@/components/ui/native-select";
import {Spinner} from "@/components/ui/spinner";
import {api,ApiError} from "@/lib/api";
import "./camera-bulk-actions.css";

type Settings = {
  siteId?: string;
  displayMode?: CameraDisplayMode;
  recordingEnabled?: boolean;
  agenda?: {always:boolean;windows:RecordingSchedule["schedule"]};
};
export type BulkCameraAction = "configure" | "activate" | "deactivate" | "delete";
export type BulkCameraResult = {changed:number;unchanged:number};
type Issue = {cameraId:string;code:string};
type Failure = {message:string;issues:Issue[]};
type Panel = "selection" | "configure" | "deactivate" | "delete" | "error";

const issueText:Record<string,string> = {
  camera_not_found:"A câmera não está mais disponível neste tenant.",
  forbidden:"Sua conta não tem permissão para aplicar esta ação.",
  camera_bulk_site_forbidden:"Sua conta não tem permissão para mover a câmera ao site de destino.",
  camera_bulk_recording_forbidden:"Sua conta não tem permissão para gerenciar a gravação no site de origem ou destino.",
  recording_profile_unavailable:"O perfil de gravação configurado está indisponível. Ajuste o perfil na câmera.",
};

export function canSelectCameras(cameras:Camera[]) {
  return cameras.some(({capabilities})=>capabilities["inventory.manage"]||capabilities["recording.manage"]||capabilities["camera.delete"]);
}

export function bulkCameraSummary(action:BulkCameraAction,{changed,unchanged}:BulkCameraResult) {
  const label = {
    configure: changed === 1 ? "câmera atualizada" : "câmeras atualizadas",
    activate: changed === 1 ? "câmera ativada" : "câmeras ativadas",
    deactivate: changed === 1 ? "câmera desativada" : "câmeras desativadas",
    delete: changed === 1 ? "câmera excluída" : "câmeras excluídas",
  }[action];
  const already = {
    configure: "já com os valores escolhidos",
    activate: unchanged === 1 ? "já estava ativa" : "já estavam ativas",
    deactivate: unchanged === 1 ? "já estava desativada" : "já estavam desativadas",
    delete: unchanged === 1 ? "já havia sido excluída" : "já haviam sido excluídas",
  }[action];
  const summary = [`${changed} ${label}`];
  if (unchanged) summary.push(`${unchanged} ${already}`);
  return summary.join(" · ") + (action === "delete" && changed ? ". As gravações serão removidas em segundo plano." : ".");
}

type Props = {
  base:string;
  cameras:Camera[];
  sites:Site[];
  selection:ReadonlySet<string>;
  outsideFilters:number;
  onSelectionChange:(selection:Set<string>)=>void;
  onComplete:(action:BulkCameraAction,result:BulkCameraResult,ids:string[])=>void;
  onBusyChange:(busy:boolean)=>void;
  fallbackFocus:()=>HTMLElement|null;
};

export function CameraBulkActions({base,cameras,sites,selection,outsideFilters,onSelectionChange,onComplete,onBusyChange,fallbackFocus}:Props) {
  const [panel,setPanel] = useState<Panel|null>(null);
  const [failure,setFailure] = useState<Failure|null>(null);
  const [busy,setBusy] = useState(false);
  const request = useRef<AbortController|null>(null);
  const trigger = useRef<HTMLElement|null>(null);
  const statusTrigger = useRef<HTMLButtonElement>(null);
  const selectionTrigger = useRef<HTMLButtonElement>(null);
  const camerasById = new Map(cameras.map(camera=>[camera.id,camera]));
  const selectedCameras = Array.from(selection,id=>camerasById.get(id));
  const canInventory = selectedCameras.some(camera=>camera?.capabilities["inventory.manage"]);
  const canRecording = selectedCameras.some(camera=>camera?.capabilities["recording.manage"]);
  const canDelete = selectedCameras.some(camera=>camera?.capabilities["camera.delete"]);
  const count = selection.size;

  useEffect(()=>()=>request.current?.abort(),[]);

  function open(next:Panel,origin:HTMLElement|null) {
    trigger.current = origin;
    setFailure(null);
    setPanel(next);
  }

  function remove(id:string) {
    const remaining = new Set(selection);
    remaining.delete(id);
    onSelectionChange(remaining);
    if (!remaining.size) {
      setPanel(null);
      setFailure(null);
      requestAnimationFrame(()=>fallbackFocus()?.focus({preventScroll:true}));
      return;
    }
    setFailure(current=>{
      const issues = current?.issues.filter(issue=>issue.cameraId!==id);
      return current&&issues?.length ? {...current,issues} : null;
    });
  }

  async function apply(action:BulkCameraAction,settings:Settings = {}) {
    if (request.current||count===0) return;
    const controller = new AbortController();
    request.current = controller;
    const ids = Array.from(selection);
    setFailure(null);
    setBusy(true);
    onBusyChange(true);
    try {
      const result = await api<BulkCameraResult>(base+"/cameras/bulk","POST",{cameraIds:ids,action,...settings},true,controller.signal);
      if (controller.signal.aborted) return;
      setPanel(null);
      onComplete(action,result,ids);
      requestAnimationFrame(()=>fallbackFocus()?.focus({preventScroll:true}));
    } catch (error) {
      if (controller.signal.aborted || (error instanceof DOMException && error.name==="AbortError")) return;
      const issues = error instanceof ApiError && error.code==="camera_bulk_rejected" && Array.isArray(error.details.issues)
        ? error.details.issues as Issue[] : [];
      setFailure({message:issues.length?"Revise as câmeras listadas para aplicar a ação ao lote.":(error instanceof Error?error.message:"Não foi possível aplicar a ação."),issues});
      if (action==="activate") {
        trigger.current = statusTrigger.current;
        setPanel("error");
      }
    } finally {
      if (!controller.signal.aborted) {
        setBusy(false);
        onBusyChange(false);
      }
      request.current = null;
    }
  }

  if (!count) return null;
  const countLabel = `${count} ${count===1?"câmera selecionada":"câmeras selecionadas"}`;
  const close = ()=>{setPanel(null);setFailure(null);};
  const cameraName = (id:string)=>camerasById.get(id)?.name??`Câmera #${id}`;
  const dialogFocus = {returnFocus:trigger.current,fallbackFocus:()=>selectionTrigger.current??fallbackFocus()};
  const errorContent = failure&&<Alert className="camera-bulk-error" variant="destructive" role="alert">
    <AlertTitle>Ação em lote não aplicada</AlertTitle>
    <AlertDescription>
      <p>{failure.message}</p>
      {!!failure.issues.length&&<ul className="camera-bulk-list">{failure.issues.map(issue=><li key={issue.cameraId}>
        <div><strong>{cameraName(issue.cameraId)}</strong><p>{issueText[issue.code]??"Confira a configuração desta câmera."}</p></div>
        <Button type="button" size="icon-sm" variant="ghost" aria-label={`Remover ${cameraName(issue.cameraId)} da seleção`} onClick={()=>remove(issue.cameraId)}><X aria-hidden="true"/></Button>
      </li>)}</ul>}
    </AlertDescription>
  </Alert>;
  const deleting = panel==="delete";

  return <>
    <section className="camera-bulk-bar" aria-label="Ações em lote de câmeras">
      <div className="camera-bulk-selection">
        <span role="status" aria-live="polite"><strong>{countLabel}</strong>{outsideFilters>0&&<small>{outsideFilters} fora dos filtros</small>}</span>
        <Button ref={selectionTrigger} type="button" variant="ghost" size="sm" disabled={busy} onClick={event=>open("selection",event.currentTarget)}>Ver seleção</Button>
      </div>
      <div className="camera-bulk-buttons">
        {canInventory&&<DropdownMenu>
          <DropdownMenuTrigger asChild><Button ref={statusTrigger} type="button" variant="outline" size="sm" disabled={busy}><Power aria-hidden="true"/>Alterar status</Button></DropdownMenuTrigger>
          <DropdownMenuContent align="center" side="top" sideOffset={8}>
            <DropdownMenuItem onSelect={()=>void apply("activate")}><Power aria-hidden="true"/>Ativar câmeras</DropdownMenuItem>
            <DropdownMenuItem onSelect={()=>open("deactivate",statusTrigger.current)}><PowerOff aria-hidden="true"/>Desativar câmeras</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>}
        {(canInventory||canRecording)&&<Button type="button" variant="outline" size="sm" disabled={busy} onClick={event=>open("configure",event.currentTarget)}><Settings aria-hidden="true"/>Configurações</Button>}
        {canDelete&&<Button type="button" variant="destructive" size="sm" disabled={busy} onClick={event=>open("delete",event.currentTarget)}><Trash2 aria-hidden="true"/>Excluir</Button>}
        {busy&&panel===null&&<span role="status" className="camera-bulk-working"><Spinner aria-hidden="true"/>Aplicando…</span>}
      </div>
      <Button className="camera-bulk-clear" type="button" variant="ghost" size="icon-sm" aria-label="Limpar seleção" title="Limpar seleção" disabled={busy} onClick={()=>{onSelectionChange(new Set());fallbackFocus()?.focus({preventScroll:true});}}><X aria-hidden="true"/></Button>
    </section>
    {panel==="selection"&&<EditorDialog title="Câmeras selecionadas" {...dialogFocus} onClose={close}>
      <ul className="camera-bulk-list">{Array.from(selection,id=><li key={id}>
        <div><strong>{cameraName(id)}</strong><p>{sites.find(site=>site.id===camerasById.get(id)?.siteId)?.name??`#${id}`}</p></div>
        <Button type="button" size="icon-sm" variant="ghost" aria-label={`Remover ${cameraName(id)} da seleção`} onClick={()=>remove(id)}><X aria-hidden="true"/></Button>
      </li>)}</ul>
    </EditorDialog>}
    {panel==="configure"&&<BulkCameraSettings {...dialogFocus} cameras={selectedCameras} countLabel={countLabel} sites={sites} canInventory={canInventory} canRecording={canRecording}
      onClose={close} busy={busy} error={errorContent} onApply={settings=>void apply("configure",settings)}/>}
    {(panel==="delete"||panel==="deactivate")&&<ConfirmationDialog {...dialogFocus} key={panel}
      title={deleting?"Excluir câmeras":"Desativar câmeras"} busy={busy}
      description={deleting?`${countLabel}. A exclusão é definitiva e as gravações serão removidas em segundo plano.`:`${countLabel}. O vídeo ao vivo e novas gravações serão interrompidos. O acervo continua disponível conforme suas permissões.`}
      confirmLabel={deleting?(busy?"Excluindo…":"Excluir câmeras"):(busy?"Desativando…":"Desativar câmeras")}
      onCancel={close} onConfirm={()=>void apply(deleting?"delete":"deactivate")}>{errorContent}</ConfirmationDialog>}
    {panel==="error"&&<EditorDialog {...dialogFocus} title="Ativar câmeras" onClose={close} busy={busy}
      footer={<Button type="button" disabled={busy} onClick={()=>void apply("activate")}>{busy?"Ativando…":"Tentar novamente"}</Button>}>
      <p>{countLabel}</p>{errorContent}
    </EditorDialog>}
  </>;
}

type EditorProps = {
  cameras:(Camera|undefined)[];sites:Site[];countLabel:string;canInventory:boolean;canRecording:boolean;
  onClose:()=>void;onApply:(settings:Settings)=>void;busy:boolean;error:ReactNode;
  returnFocus?:HTMLElement|null;fallbackFocus:()=>HTMLElement|null;
};

function BulkCameraSettings({cameras,sites,countLabel,canInventory,canRecording,onClose,onApply,busy,error,returnFocus,fallbackFocus}:EditorProps) {
  const [siteId,setSiteId] = useState("");
  const [displayMode,setDisplayMode] = useState<CameraDisplayMode|"">("");
  const [recording,setRecording] = useState("");
  const [editAgenda,setEditAgenda] = useState(false);
  const [agenda,setAgenda] = useState<RecordingSchedule>({always:true,schedule:[]});
  const destination = sites.find(site=>site.id===siteId);
  const changesTimezone = destination&&cameras.some(camera=>camera?.siteId!==siteId&&sites.find(site=>site.id===camera?.siteId)?.timezone!==destination.timezone);
  const hasChanges = !!(siteId||displayMode||recording||editAgenda);

  return <FormDialog title="Configurações em lote" wide className="camera-bulk-dialog" returnFocus={returnFocus} fallbackFocus={fallbackFocus} busy={busy} saveDisabled={!hasChanges}
    hasChanges={hasChanges} saveLabel="Aplicar alterações" busyLabel="Aplicando…" onClose={onClose} onSubmit={event=>{
      event.preventDefault();
      if (!hasChanges) return;
      onApply({
        ...(siteId?{siteId}:{}),
        ...(displayMode?{displayMode}:{}),
        ...(recording?{recordingEnabled:recording==="enabled"}:{}),
        ...(editAgenda?{agenda:{always:agenda.always,windows:agenda.always?[]:agenda.schedule}}:{}),
      });
    }}>
    <p className="camera-bulk-help">{countLabel}</p>
    {error}
    {canInventory&&<div className="form-grid camera-bulk-fields">
      <Label className="field-label">Site de destino<NativeSelect className="w-full" value={siteId} onChange={event=>setSiteId(event.target.value)}>
        <NativeSelectOption value="">Manter os sites atuais</NativeSelectOption>
        {sites.filter(site=>site.capabilities?.["inventory.manage"]).map(site=><NativeSelectOption key={site.id} value={site.id}>{site.name}{site.active?"":" · Desativado"}</NativeSelectOption>)}
      </NativeSelect></Label>
      <Label className="field-label">Modo de exibição<NativeSelect className="w-full" value={displayMode} onChange={event=>setDisplayMode(event.target.value as CameraDisplayMode|"")}>
        <NativeSelectOption value="">Manter o modo atual</NativeSelectOption>
        {Object.entries(cameraDisplayModes).map(([value,label])=><NativeSelectOption key={value} value={value}>{label}</NativeSelectOption>)}
      </NativeSelect></Label>
    </div>}
    {(changesTimezone||destination?.active===false)&&<Alert className="camera-bulk-notice" role="note"><AlertDescription>
      {changesTimezone&&<p>As agendas passam a seguir o horário local de {destination.name} ({destination.timezone}).</p>}
      {destination?.active===false&&<p>O site de destino está desativado. O ao vivo e novas gravações ficam suspensos enquanto ele estiver desativado.</p>}
    </AlertDescription></Alert>}
    {canRecording&&<section className="camera-bulk-recording" aria-label="Gravação">
      <h3>Gravação</h3>
      <Label className="field-label">Habilitação da gravação<NativeSelect className="w-full" value={recording} onChange={event=>setRecording(event.target.value)}>
        <NativeSelectOption value="">Manter a configuração atual</NativeSelectOption>
        <NativeSelectOption value="enabled">Habilitar gravação</NativeSelectOption>
        <NativeSelectOption value="disabled">Desabilitar gravação</NativeSelectOption>
      </NativeSelect></Label>
      <Label className="check"><Checkbox checked={editAgenda} onCheckedChange={value=>setEditAgenda(value===true)}/>Alterar agenda de gravação</Label>
      {editAgenda&&<>
        {!changesTimezone&&<p className="camera-bulk-help">A agenda usa o horário local de cada site.</p>}
        <RecordingScheduleEditor {...agenda} onChange={setAgenda}/>
      </>}
    </section>}
  </FormDialog>;
}
