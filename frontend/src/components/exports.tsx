"use client";

import {createContext,useContext,useEffect,useRef,useState,type FormEvent,type ReactNode} from "react";
import {CalendarDays,ChevronDown,Clock,Download,FileStack,ListTodo,Video} from "lucide-react";
import {format} from "date-fns";
import {ptBR} from "react-day-picker/locale";
import {RadioGroup} from "radix-ui";
import {ExportTasksDialog} from "@/components/export-tasks";
import {Alert,AlertDescription} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Calendar} from "@/components/ui/calendar";
import {Collapsible,CollapsibleContent,CollapsibleTrigger} from "@/components/ui/collapsible";
import {Input} from "@/components/ui/input";
import {Popover,PopoverAnchor,PopoverContent,PopoverTrigger} from "@/components/ui/popover";
import {Tooltip,TooltipContent,TooltipProvider,TooltipTrigger} from "@/components/ui/tooltip";
import {api,ApiError,mediaContext} from "@/lib/api";

type Camera = {id: string; name: string};
type Preview = {id: string; revision: number};
type Attempt = {
  cameraId: string;
  start: string;
  end: string;
  key: string;
  preview?: Preview;
};
type Submission = {cameraId: string; pending: boolean; error?: string};

const ExportContext = createContext<{
  submission: Submission | null;
  notice: number;
  openTasks: (trigger: HTMLElement) => void;
  submit: (camera: Camera, start: string, end: string) => void;
  clearError: () => void;
} | null>(null);

const exportErrors: Record<string, string> = {
  export_has_no_images: "Não há gravações nesse intervalo. Escolha outro horário.",
  export_budget_unavailable: "Não há espaço para exportar esse vídeo. Escolha um intervalo menor.",
  export_preview_expired: "O preparo expirou. Tente novamente.",
  source_changed_repreview: "Parte da gravação ficou indisponível. Tente novamente.",
  archive_requires_review: "Não foi possível preparar esse trecho. Escolha outro intervalo.",
  export_queue_full: "Há muitas exportações em andamento. Tente novamente em instantes.",
  requestor_access_revoked_or_expired: "O acesso ao vídeo ou o prazo da exportação terminou.",
  export_unavailable: "Este vídeo não está mais disponível para download.",
};

export function RecordingExportProvider({tenantId,children}:{tenantId?:string;children:ReactNode}) {
  const [submission,setSubmission] = useState<Submission|null>(null);
  const [notice,setNotice] = useState(0);
  const [tasksTrigger,setTasksTrigger] = useState<HTMLElement|null>(null);
  const attempt = useRef<Attempt>(null);
  const request = useRef<AbortController>(null);

  useEffect(() => () => request.current?.abort(), []);

  function clearError() {
    setSubmission(current => current?.pending ? current : null);
  }

  function openTasks(trigger: HTMLElement) {
    if (!tenantId) return;
    setNotice(0);
    setTasksTrigger(trigger);
  }

  async function submit(camera: Camera, start: string, end: string) {
    if (!tenantId || request.current) return;
    const setError = (error: string) => setSubmission({cameraId:camera.id,pending:false,error});
    const from = new Date(start);
    const until = new Date(end);
    if (Number.isNaN(from.getTime()) || Number.isNaN(until.getTime())) {
      setError("Preencha as datas e os horários de início e fim.");
      return;
    }
    const duration = until.getTime() - from.getTime();
    if (!(duration > 0 && duration <= 86_400_000)) {
      setError("Informe um fim posterior ao início e um intervalo de até 24 horas.");
      return;
    }

    let work = attempt.current;
    if (!work || work.cameraId !== camera.id || work.start !== start || work.end !== end) {
      const random = crypto.getRandomValues(new Uint8Array(16));
      work = {cameraId:camera.id,start,end,key:Array.from(random, byte => byte.toString(16).padStart(2,"0")).join("")};
      attempt.current = work;
    }
    const controller = new AbortController();
    const signal = AbortSignal.any([controller.signal,AbortSignal.timeout(15000)]);
    const context = mediaContext();
    const base = `/tenants/${tenantId}`;
    request.current = controller;
    setSubmission({cameraId:camera.id,pending:true});

    try {
      // Keep the validated interval and the idempotency key on a transport error.
      work.preview ??= await api<Preview>(`${base}/export-previews`,"POST",{
        cameraId:camera.id,start:from.toISOString(),end:until.toISOString(),
        timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,
      },true,signal);
      signal.throwIfAborted();
      await api(`${base}/exports`,"POST",{
        previewId:work.preview.id,revision:work.preview.revision,idempotencyKey:work.key,
      },true,signal);
      signal.throwIfAborted();
      if (!context.valid()) return;
      attempt.current = null;
      setSubmission(null);
      setNotice(count => count + 1);
    } catch (error) {
      if (controller.signal.aborted || !context.valid()) return;
      if (error instanceof ApiError && ["export_preview_expired","source_changed_repreview","export_not_found"].includes(error.code)) attempt.current = null;
      const message = error instanceof ApiError ? exportErrors[error.code] ?? error.message : "A conexão foi interrompida. Tente novamente.";
      setError(message);
    } finally {
      request.current = null;
    }
  }

  return <ExportContext.Provider value={{submission,notice,openTasks,submit,clearError}}>
    {children}
    {tenantId && tasksTrigger && <ExportTasksDialog tenantId={tenantId} trigger={tasksTrigger} onClose={()=>setTasksTrigger(null)}/>}
  </ExportContext.Provider>;
}

const timeColumns = [
  {label:"Horas",length:24},
  {label:"Minutos",length:60},
  {label:"Segundos",length:60},
].map(column=>({...column,options:Array.from({length:column.length},(_,value)=>String(value).padStart(2,"0"))}));

function ExportTimePicker({name,label,initialValue=""}:{name:string;label:string;initialValue?:string}) {
  const [value,setValue] = useState(initialValue);
  const [open,setOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const parts = value.split(":");
  const selected = timeColumns.map(({options},index)=>options.includes(parts[index]) ? parts[index] : "00");
  return <Popover open={open} onOpenChange={setOpen}>
    <PopoverAnchor asChild>
      <div className="live-export-time">
        <Input ref={inputRef} type="text" inputMode="numeric" name={name} aria-label={label}
          className="tabular-nums" placeholder="HH:mm:ss" value={value} required
          pattern="([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]" title="Informe a hora no formato HH:mm:ss, de 00:00:00 a 23:59:59."
          onClick={()=>setOpen(true)}
          onChange={({currentTarget:input})=>{
            const beforeCaret = input.value.slice(0,input.selectionStart ?? input.value.length);
            const digitsBeforeCaret = beforeCaret.replace(/\D/g,"").length;
            const formatted = input.value.replace(/\D/g,"").slice(0,6).match(/.{1,2}/g)?.join(":") ?? "";
            setValue(formatted);
            if (formatted === input.value) return;
            input.value = formatted;
            const caret = digitsBeforeCaret + Math.max(0,Math.ceil(digitsBeforeCaret/2)-1);
            input.setSelectionRange(caret,caret);
          }}/>
        <PopoverTrigger asChild>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={`Selecionar ${label.toLowerCase()}`}>
            <Clock aria-hidden="true"/>
          </Button>
        </PopoverTrigger>
      </div>
    </PopoverAnchor>
    <PopoverContent side="top" align="end" className="live-export-time-picker w-auto gap-2" aria-label={`Selecionar ${label.toLowerCase()}`}
      onOpenAutoFocus={event=>{if (document.activeElement === inputRef.current) event.preventDefault();}}
      onCloseAutoFocus={event=>{if (document.activeElement === inputRef.current) event.preventDefault();}}
      onInteractOutside={event=>{if (event.target === inputRef.current) event.preventDefault();}}>
      <div className="live-export-time-columns">
        {timeColumns.map(({label,options},index)=><div key={label} className="live-export-time-column">
          <span aria-hidden="true">{label}</span>
          <RadioGroup.Root aria-label={label} orientation="vertical" value={selected[index]}
            onValueChange={part=>setValue(selected.map((current,i)=>i === index ? part : current).join(":"))}>
            {options.map(part=><RadioGroup.Item key={part} value={part} asChild>
              <Button type="button" variant={part === selected[index] ? "secondary" : "ghost"} size="sm"
                ref={node=>{
                  const list = node?.parentElement;
                  if (node && list && part === selected[index]) {
                    list.scrollTop = node.offsetTop-(list.clientHeight-node.offsetHeight)/2;
                  }
                }}>{part}</Button>
            </RadioGroup.Item>)}
          </RadioGroup.Root>
        </div>)}
      </div>
      <Button type="button" size="sm" onClick={()=>setOpen(false)}>Concluir</Button>
    </PopoverContent>
  </Popover>;
}

function ExportDateTime({name,label,value}:{name:string;label:string;value?:string}) {
  const [initialDate,time] = value?.split("T") ?? [];
  const [date,setDate] = useState(initialDate ?? "");
  const [open,setOpen] = useState(false);
  const selected = date ? new Date(`${date}T12:00:00`) : undefined;
  const numericDate = date ? date.split("-").reverse().join("/") : "dd/mm/aaaa";
  return <div className="live-export-datetime" role="group" aria-label={label}>
    <span>{label}</span>
    <input type="hidden" name={`${name}Date`} value={date}/>
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" className="live-export-date-trigger" data-empty={!date}
          aria-label={`Data de ${label.toLowerCase()}, ${numericDate}`}>
          <CalendarDays aria-hidden="true"/>{numericDate}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" side="top" className="w-auto p-0" aria-label={`Calendário de ${label.toLowerCase()}`}>
        <Calendar mode="single" required locale={ptBR} selected={selected} defaultMonth={selected} autoFocus
          formatters={{formatCaption:month=>format(month,"MM/yyyy")}}
          onSelect={day=>{
            setDate(format(day,"yyyy-MM-dd"));
            setOpen(false);
          }}/>
      </PopoverContent>
    </Popover>
    <ExportTimePicker name={`${name}Time`} label={`Hora de ${label.toLowerCase()}`} initialValue={time}/>
  </div>;
}

function ExportContents({camera,allowed,initialInterval}:{camera:Camera;allowed:boolean;initialInterval:{start:string;end:string}}) {
  const exportAction = useContext(ExportContext);
  if (!exportAction) return null;
  const {submit:startExport,submission} = exportAction;
  const error = submission?.cameraId === camera.id ? submission.error : undefined;
  const pending = submission?.pending ?? false;
  const cameraName = <p className="live-export-camera" title={camera.name}><Video aria-hidden="true"/><span>{camera.name}</span></p>;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const value = (name: string) => `${fields.get(`${name}Date`)}T${fields.get(`${name}Time`)}`;
    startExport(camera,value("start"),value("end"));
  }

  if (!allowed) return <>{cameraName}<p className="text-muted-foreground">Sua conta não pode exportar esta câmera.</p></>;

  const feedback = error && <Alert variant="destructive" role="alert" className="alert"><AlertDescription>{error}</AlertDescription></Alert>;
  return <>{cameraName}
    <form key={camera.id} onSubmit={submit} aria-busy={pending}>
      {feedback}
      <div className="live-export-fields">
        <ExportDateTime name="start" label="Início" value={initialInterval.start}/>
        <ExportDateTime name="end" label="Fim" value={initialInterval.end}/>
      </div>
      <div className="live-export-actions">
        <Button type="submit" size="sm" disabled={pending}><Download aria-hidden="true"/>
          {pending ? "Solicitando…" : error ? "Tentar novamente" : "Exportar"}
        </Button>
        <TooltipProvider delayDuration={250}><Tooltip>
          <TooltipTrigger asChild><span className="inline-flex rounded-sm" tabIndex={0}>
            <Button type="button" variant="outline" size="sm" aria-label="Exportar em grupo" disabled><FileStack aria-hidden="true"/></Button>
          </span></TooltipTrigger>
          <TooltipContent>Exportar em grupo</TooltipContent>
        </Tooltip></TooltipProvider>
      </div>
    </form>
  </>;
}

function TaskWindowButton() {
  const exportAction = useContext(ExportContext);
  if (!exportAction) return null;
  const {notice,openTasks} = exportAction;
  return <>
    <Button type="button" variant="outline" size="sm" className="export-task-trigger live-export-task-trigger"
      aria-haspopup="dialog" aria-label="Janela de tarefas" onClick={event=>openTasks(event.currentTarget)}>
      <ListTodo aria-hidden="true"/>Janela de tarefas
      {notice > 0 && <span key={notice} className="export-task-indicator" aria-hidden="true"/>}
    </Button>
    <span className="sr-only" role="status">{notice === 1 ? "Nova tarefa criada." : notice > 1 ? `${notice} novas tarefas criadas.` : ""}</span>
  </>;
}

export function LiveRecordingExportPanel({camera,allowed}:{camera?: Camera;allowed:boolean}) {
  const [initialInterval,setInitialInterval] = useState<{start:string;end:string}>();
  const exportAction = useContext(ExportContext);
  if (!exportAction) return null;
  return <Collapsible className="live-export-panel" onOpenChange={open=>{
    if (!open) {
      setInitialInterval(undefined);
      return;
    }
    const now = new Date();
    const tenMinutesAgo = new Date(now.getTime()-10*60_000);
    const localTimestamp = "yyyy-MM-dd'T'HH:mm:ss";
    setInitialInterval({start:format(tenMinutesAgo,localTimestamp),end:format(now,localTimestamp)});
    exportAction.clearError();
  }}>
    <CollapsibleTrigger className="live-camera-site-trigger live-export-trigger">
      <Download aria-hidden="true"/>
      <span>Exportação</span>
      <ChevronDown className="live-camera-trigger-chevron" aria-hidden="true"/>
    </CollapsibleTrigger>
    <CollapsibleContent className="live-export-content" role="region" aria-label="Exportação">
      {initialInterval && (camera ? <ExportContents camera={camera} allowed={allowed} initialInterval={initialInterval}/> :
        <p className="live-export-empty">Nenhuma câmera selecionada.</p>)}
      <TaskWindowButton/>
    </CollapsibleContent>
  </Collapsible>;
}
